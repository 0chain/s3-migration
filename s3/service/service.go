package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/0chain/s3migration/model"
	"github.com/0chain/s3migration/s3"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	awsS3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"golang.org/x/sync/errgroup"
	"log"
	"path"
	"sync"
)

var existingFiles map[string]*model.FileRef

func SetExistingFileList(dStorageFiles map[string]*model.FileRef) {
	existingFiles = dStorageFiles
}

type Service struct {
	clientMap map[string]*awsS3.Client
}

func NewService(region string) (*Service, error) {
	clientMap := make(map[string]*awsS3.Client)
	if len(region) == 0 {
		region = "us-east-1"
	}

	var cfg aws.Config
	cfg, err := awsConfig.LoadDefaultConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("configuration error " + err.Error() + "region: " + region)
	}

	cfg.Region = region
	client := awsS3.NewFromConfig(cfg)
	clientMap[region] = client

	return &Service{
		clientMap: clientMap,
	}, nil
}

func (s *Service) InitClientWithRegion(region string) error {
	cfg, err := awsConfig.LoadDefaultConfig(context.Background())
	if err != nil {
		return fmt.Errorf("configuration error " + err.Error() + "region: " + region)
	}
	cfg.Region = region
	client := awsS3.NewFromConfig(cfg)
	s.clientMap[region] = client

	return nil
}

func (s *Service) ListAllBuckets(ctx context.Context) ([]string, error) {
	var err error
	var bucketList []string
	g, groupCtx := errgroup.WithContext(ctx)
	mutex := &sync.Mutex{}
	for _, c := range s.clientMap {
		client := c
		g.Go(func() error {
			buckets, err := client.ListBuckets(groupCtx, &awsS3.ListBucketsInput{})
			if err != nil {
				return err
			}

			mutex.Lock()
			for _, b := range buckets.Buckets {
				if b.Name != nil {
					bucketList = append(bucketList, *b.Name)
				}
			}
			mutex.Unlock()
			return nil
		})
	}

	if err = g.Wait(); err != nil {
		return bucketList, err
	}

	return bucketList, nil
}

func (s *Service) getBucketRegion(ctx context.Context, bucketName string, client *awsS3.Client) (string, error) {
	locationInfo, err := client.GetBucketLocation(ctx, &awsS3.GetBucketLocationInput{
		Bucket: &bucketName,
	})
	if err != nil {
		return "", err
	}

	return string(locationInfo.LocationConstraint), nil
}

func (s *Service) getClient(location string) (*awsS3.Client, error) {
	if location == "" {
		for _, c := range s.clientMap {
			if c != nil {
				return c, nil
			}
		}
	} else {
		if c, ok := s.clientMap[location]; ok {
			return c, nil
		}
	}

	return nil, fmt.Errorf("client not initialized")
}

func (s *Service) GetBucketRegion(ctx context.Context, bucketList []string) ([]s3.Bucket, error) {
	client, err := s.getClient("")
	if err != nil {
		return nil, err
	}

	var bucketLocationData []s3.Bucket
	type locError struct {
		err        error
		bucketName string
	}
	var locErr []locError
	g, groupCtx := errgroup.WithContext(ctx)
	mutex := &sync.Mutex{}
	for _, b := range bucketList {
		bucket := b
		g.Go(func() error {
			locDetail, err := client.GetBucketLocation(groupCtx, &awsS3.GetBucketLocationInput{Bucket: &bucket})
			if err != nil {
				locErr = append(locErr, locError{err: err, bucketName: bucket})
				return nil
			}

			location := string(locDetail.LocationConstraint)
			mutex.Lock()
			bucketLocationData = append(bucketLocationData, s3.Bucket{
				Name:     bucket,
				Location: location,
			})

			if _, ok := s.clientMap[location]; !ok {
				if err := s.InitClientWithRegion(location); err != nil {
					locErr = append(locErr, locError{err: err, bucketName: bucket})
				}
			}
			mutex.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return bucketLocationData, err
	}

	if len(locErr) > 0 {
		for _, loc := range locErr {
			log.Printf("errors fetching loc data bucketName: %s, err : %+v", loc.bucketName, loc.err)
		}
	}

	if len(locErr) == len(bucketList) {
		return bucketLocationData, errors.New("error fetching location Data")
	}

	return bucketLocationData, nil
}

func (s *Service) ListFilesInBucket(ctx context.Context, opts model.ListFileOptions) (map[string]int64, error) {
	client, err := s.getClient(opts.Region)
	if err != nil {
		return nil, err
	}
	bucketFiles := map[string]int64{}

	log.Println("contents of bucket : ", opts.Bucket)

	listObjectsInput := &awsS3.ListObjectsV2Input{
		Bucket: &opts.Bucket,
	}
	if len(opts.Prefix) != 0 {
		listObjectsInput.Prefix = &opts.Prefix
	}

	maxKeys := int32(1000)
	pageNumber := 0

	listObjectsPaginator := awsS3.NewListObjectsV2Paginator(client, listObjectsInput, func(o *awsS3.ListObjectsV2PaginatorOptions) {
		if v := maxKeys; v != 0 {
			o.Limit = v
		}
	})

	for listObjectsPaginator.HasMorePages() {
		pageNumber++
		page, err := listObjectsPaginator.NextPage(ctx)
		if err != nil {
			log.Fatalf("failed to get page %v, %v", pageNumber, err)
		}

		for _, obj := range page.Contents {
			if obj.Size == 0 {
				continue
			}

			isUpdate := false
			filePath := fmt.Sprintf("/%s", path.Join(opts.Bucket, aws.ToString(obj.Key)))
			if existingFiles[filePath] != nil {
				log.Println("duplicate file found with full path: ", filePath)
				if existingFiles[filePath].Size == obj.Size && existingFiles[filePath].ModifiedAt.Unix() > aws.ToTime(obj.LastModified).Unix() {
					continue
				}
				isUpdate = true
			}

			log.Println("Enqueuing this file to be uploaded:", aws.ToString(obj.Key), "isUpdate:", isUpdate)
			opts.WaitGroup.Add(1)
			opts.FileQueue <- model.FileRef{
				Path:       filePath,
				Bucket:     opts.Bucket,
				Region:     opts.Region,
				Key:        aws.ToString(obj.Key),
				Size:       obj.Size,
				ModifiedAt: aws.ToTime(obj.LastModified),
				IsUpdate:   isUpdate,
			}
		}

	}

	return bucketFiles, nil
}

func (s *Service) GetFile(ctx context.Context, opts model.GetFileOptions) (*model.S3Object, error) {
	client, err := s.getClient(opts.Region)
	if err != nil {
		return nil, err
	}

	out, err := client.GetObject(ctx, &awsS3.GetObjectInput{Bucket: aws.String(opts.Bucket), Key: aws.String(opts.Key)})
	if err != nil {
		return nil, err
	}

	res := &model.S3Object{
		SourceFile: out.Body,
		FileType:   aws.ToString(out.ContentType),
		FileSize:   out.ContentLength,
		FilePath:   fmt.Sprintf("/%s", path.Join(opts.Bucket, opts.Key)),
	}

	return res, nil
}
