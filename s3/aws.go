package s3

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	zlogger "github.com/0chain/s3migration/logger"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	awsS3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

type AwsI interface {
	ListFilesInBucket(ctx context.Context) (<-chan *ObjectMeta, <-chan error)
	GetFileContent(ctx context.Context, objectKey string) (*Object, error)
	DeleteFile(ctx context.Context, objectKey string) error
}

type Object struct {
	Body          io.Reader
	ContentType   string
	ContentLength int64
}

// ObjectMeta key: object key, size: size of object in bytes
type ObjectMeta struct {
	Key  string
	Size int64
}

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
}

func GetAwsClient(bucket, prefix, region string, deleteSource bool, newerThan, olderThan *time.Time, startAfter, workDir string) (*AwsClient, error) {
	//Get a client; if error return error else return aws client
	//buckets comes as slice of array([bucketname, prefix]). Find location and put all of them
	//in buckets field. If bucket is nil; then list all buckets from s3 and update the buckets field
	// For example
	// for _, bkt := range buckets{
	// 	bucketName := bkt[0]
	// 	prefix := bkt[1]
	// 	location := "abc" // get from client
	// 	awsClient.buckets = append(awsClient.buckets, bucket{
	// 		Name: bucketName,
	// 		Prefix: prefix,
	// 		Location: location,
	// 	})
	// }

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

func (a *AwsClient) ListFilesInBucket(ctx context.Context) (<-chan *ObjectMeta, <-chan error) {
	objectMetaChan := make(chan *ObjectMeta, 1000)
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
				creationTime := aws.ToTime(obj.LastModified)
				if a.newerThan != nil && creationTime.Before(*a.newerThan) {
					continue
				}

				if a.olderThan != nil && creationTime.After(*a.olderThan) {
					continue
				}

				objectMetaChan <- &ObjectMeta{Key: aws.ToString(obj.Key), Size: obj.Size}
			}
		}
	}()
	return objectMetaChan, errChan
}

func (a *AwsClient) GetFileContent(ctx context.Context, objectKey string) (*Object, error) {
	out, err := a.client.GetObject(ctx, &awsS3.GetObjectInput{Bucket: aws.String(a.bucket), Key: aws.String(objectKey)})
	if err != nil {
		return nil, err
	}

	return &Object{
		Body:          out.Body,
		ContentType:   aws.ToString(out.ContentType),
		ContentLength: out.ContentLength,
	}, nil
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
