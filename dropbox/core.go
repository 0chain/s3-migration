package dropbox

import (
	"context"
	"fmt"
	"io"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	zlogger "github.com/0chain/s3migration/logger"
	T "github.com/0chain/s3migration/types"
	"github.com/pkg/errors"

	"github.com/dropbox/dropbox-sdk-go-unofficial/v6/dropbox"
	"github.com/dropbox/dropbox-sdk-go-unofficial/v6/dropbox/files"
)

type DropboxClient struct {
	token        string
	workDir      string
	dropboxConf  *dropbox.Config
	dropboxFiles files.Client
	newerThan    *time.Time
	olderThan    *time.Time
}

func GetDropboxClient(token string, workDir string, newerThan *time.Time, olderThan *time.Time) (*DropboxClient, error) {
	config := dropbox.Config{
		Token: token,
	}

	client := files.New(config)

	// for invalid Access token
	arg := files.NewListFolderArg("")

	_, err := client.ListFolder(arg)

	if err != nil {
		return nil, errors.Wrap(err, "invalid Client token")
	}

	return &DropboxClient{
		token:        token,
		dropboxConf:  &config,
		dropboxFiles: client,
		workDir:      workDir,
		newerThan:    newerThan,
		olderThan:    olderThan,
	}, nil
}

func (d *DropboxClient) ListFiles(ctx context.Context) (<-chan *T.ObjectMeta, <-chan error) {
	objectChan := make(chan *T.ObjectMeta)
	errChan := make(chan error)

	go func() {
		defer func() {
			close(objectChan)
			close(errChan)
		}()

		arg := files.NewListFolderArg("") // "" for Root
		arg.Recursive = true
		arg.Limit = 100
		arg.IncludeNonDownloadableFiles = true

		res, err := d.dropboxFiles.ListFolder(arg)
		if err != nil {
			errChan <- err
			return
		}

		isNonDownloadableFile := func(meta *files.FileMetadata) bool {
			return strings.HasPrefix(meta.ContentHash, "paper") ||
				strings.Contains(meta.PathLower, ".paper") ||
				!meta.IsDownloadable
		}

		processEntries := func(entries []files.IsMetadata) {
			for _, entry := range entries {
				if meta, ok := entry.(*files.FileMetadata); ok {
					lastModified := meta.ClientModified

					if (d.newerThan == nil || d.newerThan.Unix() == 0 || lastModified.Unix() >= d.newerThan.Unix()) &&
						(d.olderThan == nil || d.olderThan.Unix() == 0 || lastModified.Unix() <= d.olderThan.Unix()) {

						ext := filepath.Ext(meta.PathDisplay)
						name := meta.Name
						zlogger.Logger.Info(meta)
						zlogger.Logger.Info("file information")
						size := meta.Size
						if isNonDownloadableFile(meta) {
							ext = ".txt"
							if !strings.HasSuffix(name, ".txt") {
								name += ".txt"
							}
							zlogger.Logger.Info(fmt.Sprintf("Non-downloadable file detected: %s, will be exported as text", meta.Name))
							size = 0
						}

						objectChan <- &T.ObjectMeta{
							Key:          meta.PathDisplay,
							Size:         int64(size),
							ContentType:  mime.TypeByExtension(ext),
							Ext:          ext,
							Name:         &name,
							ParentPath:   nil,  // object key holds file path
						}
					}
				}
			}
		}

		// Process initial entries
		processEntries(res.Entries)

		// Continue processing if there are more entries
		cursor := res.Cursor
		hasMore := res.HasMore

		for hasMore {
			continueArg := files.NewListFolderContinueArg(cursor)
			res, err := d.dropboxFiles.ListFolderContinue(continueArg)
			if err != nil {
				errChan <- err
				return
			}

			processEntries(res.Entries)

			cursor = res.Cursor
			hasMore = res.HasMore
		}
	}()
	return objectChan, errChan
}

func (d *DropboxClient) GetFileContent(ctx context.Context, filePath string) (*T.Object, error) {
	arg := files.NewDownloadArg(filePath)
	res, content, err := d.dropboxFiles.Download(arg)
	if err != nil {
		return nil, err
	}

	return &T.Object{
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
	downloadPath := path.Join(d.workDir, fileName)
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
