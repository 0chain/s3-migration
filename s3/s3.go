package s3

import (
	"context"
	"github.com/0chain/s3migration/model"
)

type Bucket struct {
	Name     string
	Location string
}

type S3 interface {
	ListAllBuckets(ctx context.Context) ([]string, error)
	GetBucketRegion(ctx context.Context, bucketList []string) ([]Bucket, error)
	ListFilesInBucket(ctx context.Context, opts model.ListFileOptions) (map[string]int64, error)
	GetFile(ctx context.Context, opts model.GetFileOptions) (*model.S3Object, error)
}
