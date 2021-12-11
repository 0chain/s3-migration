package model

import (
	"io"
	"sync"
	"time"
)

type AppConfig struct {
	Skip          int
	Resume        bool
	Concurrency   int
	Buckets       []string
	Region        string
	MigrateToPath string
	WhoPays       string
	Encrypt       bool
	DeleteSource  bool
}

type FileRef struct {
	Path      string
	Bucket    string
	Region    string
	Key       string
	Size      int64
	IsUpdate  bool
	UpdatedAt time.Time
}

type ListFileOptions struct {
	Bucket    string
	Prefix    string
	Region    string
	FileQueue chan FileRef
	WaitGroup *sync.WaitGroup
}

type GetFileOptions struct {
	Bucket string
	Region string
	Key    string
}

type DeleteFileOptions struct {
	Bucket string
	Region string
	Key    string
}

type S3Object struct {
	SourceFile io.Reader
	FileType   string
	FileSize   int64
	FilePath   string
}
