package util

import (
	"io"
	"os"
)

var Fs FileSystem = osFS{}

//go:generate mockgen -destination mocks/mock_filesystem.go -package mock_util github.com/0chain/s3migration/util FileSystem
type FileSystem interface {
	Open(name string) (File, error)
	Stat(name string) (os.FileInfo, error)
	Remove(name string) error
}

//go:generate mockgen -destination mocks/mock_file.go -package mock_util github.com/0chain/s3migration/util File
type File interface {
	io.Closer
	io.Reader
	io.ReaderAt
	io.Seeker
	Stat() (os.FileInfo, error)
}

type osFS struct{}

func (osFS) Open(name string) (File, error)        { return os.Open(name) }
func (osFS) Stat(name string) (os.FileInfo, error) { return os.Stat(name) }
func (osFS) Remove(name string) error              { return os.Remove(name) }

//go:generate mockgen -destination mocks/mock_file_info.go -package mock_util github.com/0chain/s3migration/util FileInfo
type FileInfo os.FileInfo
