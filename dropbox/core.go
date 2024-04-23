package dropbox

import (
	"context"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"

	"github.com/0chain/s3migration/header"
	"github.com/dropbox/dropbox-sdk-go-unofficial/v6/dropbox"
	"github.com/dropbox/dropbox-sdk-go-unofficial/v6/dropbox/files"
)

type DropboxI interface {
	header.CloudStorageI
}

type Object struct {
	Body          io.ReadCloser
	ContentType   string
	ContentLength int64
}

type ObjectMeta struct {
	Key         string
	Size        int64
	ContentType string
}

type DropboxClient struct {
	token        string
	dropboxConf  *dropbox.Config
	dropboxFiles files.Client
}

func GetDropboxClient(token string) (*DropboxClient, error) {
	config := dropbox.Config{
		Token: token,
	}

	client := files.New(config)

	return &DropboxClient{
		token:        token,
		dropboxConf:  &config,
		dropboxFiles: client,
	}, nil
}

func (d *DropboxClient) ListFiles(ctx context.Context) (<-chan *ObjectMeta, <-chan error) {
	objectChan := make(chan *ObjectMeta)
	errChan := make(chan error)

	go func() {
		defer close(objectChan)
		defer close(errChan)

		arg := files.NewListFolderArg("")
		arg.Recursive = true

		res, err := d.dropboxFiles.ListFolder(arg)
		if err != nil {
			errChan <- err
			return
		}

		for _, entry := range res.Entries {
			if meta, ok := entry.(*files.FileMetadata); ok {
				objectChan <- &ObjectMeta{
					Key:         meta.PathDisplay,
					Size:        int64(meta.Size),
					ContentType: mime.TypeByExtension(filepath.Ext(meta.PathDisplay)),
				}
			}
		}
	}()

	return objectChan, errChan
}

func (d *DropboxClient) GetFileContent(ctx context.Context, filePath string) (*Object, error) {
	arg := files.NewDownloadArg(filePath)
	res, content, err := d.dropboxFiles.Download(arg)
	if err != nil {
		return nil, err
	}

	return &Object{
		Body:          content,
		ContentType:   mime.TypeByExtension(filepath.Ext(filePath)),
		ContentLength: int64(res.Size),
	}, nil
}

func (d *DropboxClient) DeleteFile(ctx context.Context, filePath string) error {
	arg := files.NewDeleteArg(filePath)
	_, err := d.dropboxFiles.DeleteV2(arg)
	return err
}

func (d *DropboxClient) DownloadToFile(ctx context.Context, filePath string) (string, error) {
	arg := files.NewDownloadArg(filePath)
	_, content, err := d.dropboxFiles.Download(arg)
	if err != nil {
		return "", err
	}

	fileName := filepath.Base(filePath)
	downloadPath := filepath.Join(".", fileName)
	file, err := os.Create(downloadPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	_, err = io.Copy(file, content)
	if err != nil {
		return "", err
	}

	return downloadPath, nil
}

func (d *DropboxClient) DownloadToMemory(ctx context.Context, objectKey string, offset int64, chunkSize, objectSize int64) ([]byte, error) {
	limit := offset + chunkSize - 1
	if limit > objectSize {
		limit = objectSize
	}

	rng := fmt.Sprintf("bytes=%d-%d", offset, limit)

	arg := files.NewDownloadArg(objectKey)

	arg.ExtraHeaders = map[string]string{"Range": rng}

	_, content, err := d.dropboxFiles.Download(arg)
	if err != nil {
		return nil, err
	}
	defer content.Close()

	data := make([]byte, chunkSize)
	n, err := io.ReadFull(content, data)

	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, err
	}

	if int64(n) < chunkSize && objectSize != chunkSize {
		data = data[:n]
	}

	return data, nil
}
