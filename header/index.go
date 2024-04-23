package header

import (
	"context"
	"io"
)

type Object struct {
	Body          io.Reader
	ContentType   string
	ContentLength int64
}

// ObjectMeta key: object key, size: size of object in bytes
type ObjectMeta struct {
	Key         string
	Size        int64
	ContentType string
}

type CloudStorageI interface {
	ListFiles(ctx context.Context) (<-chan *ObjectMeta, <-chan error)
	GetFileContent(ctx context.Context, objectKey string) (*Object, error)
	DeleteFile(ctx context.Context, objectKey string) error
	DownloadToFile(ctx context.Context, objectKey string) (string, error)
	DownloadToMemory(ctx context.Context, objectKey string, offset int64, chunkSize, objectSize int64) ([]byte, error)
}

type CloudStorageClient struct {
	name string
}
