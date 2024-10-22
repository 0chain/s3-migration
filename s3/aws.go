package s3

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	T "github.com/0chain/s3migration/types"

	"github.com/0chain/gosdk/core/encryption"
	zlogger "github.com/0chain/s3migration/logger"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	awsS3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

type AwsClient struct {
	bucket       string
	prefix       string
	region       string
	startAfter   string
	workDir      string
	deleteSource bool
	newerThan    *time.Time
	olderThan    *time.Time
	client       *awsS3.Client
	downloader   *manager.Downloader
}

func GetAwsClient(bucket, prefix, region string, deleteSource bool, newerThan, olderThan *time.Time, startAfter, workDir string) (*AwsClient, error) {

	if region == "" {
		region = "us-east-1"
	}
	workDir = filepath.Join(workDir, "s3")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return nil, err
	}

	awsClient := &AwsClient{
		bucket:       bucket,
		prefix:       prefix,
		region:       region,
		startAfter:   startAfter,
		deleteSource: deleteSource,
		newerThan:    newerThan,
		olderThan:    olderThan,
		workDir:      workDir,
	}

	var err error
	awsClient.client, err = getAwsSDKClient(awsClient.region)
	if err != nil {
		return nil, err
	}

	awsClient.region, err = awsClient.getBucketRegion()
	if err != nil {
		return nil, err
	}

	if region != awsClient.region {
		awsClient.client, err = getAwsSDKClient(awsClient.region)
		if err != nil {
			return nil, err
		}
	}

	awsClient.downloader = manager.NewDownloader(awsClient.client, func(u *manager.Downloader) {
		u.PartSize = 5 * 1024 * 1024
		u.Concurrency = 100
	})

	zlogger.Logger.Info(fmt.Sprintf(
		"Aws client initialized with"+
			"bucket: %v,"+
			"prefix: %v,"+
			"region: %v,"+
			"startAfter: %v,"+
			"deleteSource: %v,"+
			"newerThan: %v,"+
			"olderThan: %v,"+
			"workDir: %v", bucket, prefix, region, startAfter, deleteSource, newerThan, olderThan, workDir))
	return awsClient, nil
}

func getAwsSDKClient(region string) (*awsS3.Client, error) {
	var cfg aws.Config
	cfg, err := awsConfig.LoadDefaultConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("configuration error " + err.Error() + "region: " + region)
	}

	cfg.Region = region
	client := awsS3.NewFromConfig(cfg)
	return client, nil
}

func (a *AwsClient) getBucketRegion() (region string, err error) {
	locationInfo, err := a.client.GetBucketLocation(context.Background(), &awsS3.GetBucketLocationInput{
		Bucket: &a.bucket,
	})
	if err != nil {
		return
	}

	region = string(locationInfo.LocationConstraint)
	if region == "" {
		region = "us-east-1"
	}
	return
}

func (a *AwsClient) ListFiles(ctx context.Context) (<-chan *T.ObjectMeta, <-chan error) {
	objectMetaChan := make(chan *T.ObjectMeta, 1000)
	errChan := make(chan error, 1)

	go func() {
		defer func() {
			close(objectMetaChan)
			close(errChan)
		}()

		listObjectsInput := &awsS3.ListObjectsV2Input{
			Bucket: &a.bucket,
		}
		if len(a.prefix) != 0 {
			listObjectsInput.Prefix = &a.prefix
		}

		if len(a.startAfter) != 0 {
			listObjectsInput.StartAfter = &a.startAfter
		}

		maxKeys := int32(1000)
		pageNumber := 0

		listObjectsPaginator := awsS3.NewListObjectsV2Paginator(a.client, listObjectsInput, func(o *awsS3.ListObjectsV2PaginatorOptions) {
			if v := maxKeys; v != 0 {
				o.Limit = v
			}
		})

		for listObjectsPaginator.HasMorePages() {
			pageNumber++
			page, err := listObjectsPaginator.NextPage(ctx)
			if err != nil {
				errChan <- err
				return
			}

			for _, obj := range page.Contents {
				if strings.HasSuffix(aws.ToString(obj.Key), "/") {
					zlogger.Logger.Info("Skipping prefix migration")
					continue
				}
				creationTime := aws.ToTime(obj.LastModified)
				if a.newerThan != nil && creationTime.Before(*a.newerThan) {
					continue
				}

				if a.olderThan != nil && creationTime.After(*a.olderThan) {
					continue
				}
				contentType, err := a.GetContentType(ctx, aws.ToString(obj.Key))
				if err != nil {
					errChan <- err
					return
				}
				objectMetaChan <- &T.ObjectMeta{Key: aws.ToString(obj.Key), Size: obj.Size, ContentType: contentType}
			}
		}
	}()
	return objectMetaChan, errChan
}

func (a *AwsClient) GetFileContent(ctx context.Context, objectKey string) (*T.Object, error) {
	out, err := a.client.GetObject(ctx, &awsS3.GetObjectInput{Bucket: aws.String(a.bucket), Key: aws.String(objectKey)})
	if err != nil {
		return nil, err
	}

	return &T.Object{
		Body:          out.Body,
		ContentType:   aws.ToString(out.ContentType),
		ContentLength: out.ContentLength,
	}, nil
}

func (a *AwsClient) GetContentType(ctx context.Context, objectKey string) (string, error) {
	out, err := a.client.HeadObject(ctx, &awsS3.HeadObjectInput{Bucket: aws.String(a.bucket), Key: aws.String(objectKey)})
	if err != nil {
		return "", err
	}
	return aws.ToString(out.ContentType), nil
}

func (a *AwsClient) DeleteFile(ctx context.Context, objectKey string) error {
	if !a.deleteSource {
		return nil
	}
	_, err := a.client.DeleteObject(ctx, &awsS3.DeleteObjectInput{
		Bucket: aws.String(a.bucket),
		Key:    aws.String(objectKey),
	})
	return err
}

func (a *AwsClient) DownloadToFile(ctx context.Context, objectKey string) (string, error) {
	params := &awsS3.GetObjectInput{
		Bucket: aws.String(a.bucket),
		Key:    aws.String(objectKey),
	}
	fileName := encryption.Hash(objectKey)
	downloadPath := filepath.Join(a.workDir, fileName)
	f, err := os.Create(downloadPath)
	if err != nil {
		return downloadPath, err
	}

	defer f.Close()
	_, err = a.downloader.Download(ctx, f, params)
	return downloadPath, err
}

func (a *AwsClient) DownloadToMemory(ctx context.Context, objectKey string, offset int64, chunkSize, objectSize int64) ([]byte, error) {
	limit := offset + chunkSize - 1
	if offset+chunkSize-1 > objectSize {
		limit = objectSize
	}
	ran := fmt.Sprintf("bytes=%d-%d", offset, limit)
	params := &awsS3.GetObjectInput{
		Bucket: aws.String(a.bucket),
		Key:    aws.String(objectKey),
		Range:  &ran,
	}
	maxSize := chunkSize
	bytearray := make([]byte, 0, maxSize)
	buffer := manager.NewWriteAtBuffer(bytearray)

	n, err := a.downloader.Download(ctx, buffer, params)
	return buffer.Bytes()[:n], err
}
