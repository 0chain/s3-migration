package gdrive

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"time"

	zlogger "github.com/0chain/s3migration/logger"
	T "github.com/0chain/s3migration/types"
	"github.com/pkg/errors"
	"golang.org/x/oauth2"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

type GoogleDriveClient struct {
	service *drive.Service
	workDir string
	newerThan *time.Time
	olderThan *time.Time
}

func NewGoogleDriveClient(cfg oauth2.Config, token *oauth2.Token, workDir string, newerThan *time.Time, olderThan *time.Time) (*GoogleDriveClient, error) {
	ctx := context.Background()
	var httpClient *http.Client

	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		tokenSource := oauth2.StaticTokenSource(&oauth2.Token{
			AccessToken:  driveAccessToken,
			RefreshToken: driveRefreshToken,
		})
		httpClient = oauth2.NewClient(ctx, tokenSource)
	} else {
		httpClient = cfg.Client(ctx, token)
	}

	service, err := drive.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, err
	}
	_, err = service.Files.List().Do()

	if err != nil {
		return nil, errors.Wrap(err, "invalid Google Drive access token")
	}

	return &GoogleDriveClient{
		service: service,
		workDir: workDir,
		newerThan: newerThan,
		olderThan: olderThan,
	}, nil
}

func (g *GoogleDriveClient) ListFiles(ctx context.Context) (<-chan *T.ObjectMeta, <-chan error) {
	objectChan := make(chan *T.ObjectMeta)
	errChan := make(chan error)

	go func() {
		defer func() {
			close(objectChan)
			close(errChan)
		}()

		filesReq := g.service.Files.List().Context(ctx)

		filesReq.Q("trashed=false")

		filesReq.Fields(
			"files(id, mimeType, size,fileExtension, name, modifiedTime)",
		)

		filesReq.Pages(ctx, func(page *drive.FileList) error {
			return nil
		})

		filesReq.PageSize(100)

		files, err := filesReq.Do()
		if err != nil {
			errChan <- err
			return
		}

		for _, file := range files.Files {
			lastModified, err := time.Parse(time.RFC3339, file.ModifiedTime)

			if err != nil {
				zlogger.Logger.Error(err)
				continue
			}

			if (g.newerThan == nil || g.newerThan.Unix() == 0 || lastModified.Unix() >= g.newerThan.Unix()) &&
				(g.olderThan == nil || g.olderThan.Unix() == 0 || lastModified.Unix() <= g.olderThan.Unix()) {
				objectChan <- &T.ObjectMeta{
					Key:         file.Name,
					Size:        file.Size,
					ContentType: file.MimeType,
					Ext:         file.FileExtension,
				}
			}
		}

		nextPgToken := files.NextPageToken

		for nextPgToken != "" {
			filesReq.PageToken(nextPgToken)

			files, err := filesReq.Do()

			if err != nil {
				errChan <- err
				return
			}

			for _, file := range files.Files {
				objectChan <- &T.ObjectMeta{
					Key:         file.Name,
					Size:        file.Size,
					ContentType: file.MimeType,
				}
			}

			nextPgToken = files.NextPageToken
		}
	}()

	return objectChan, errChan
}

func (g *GoogleDriveClient) GetFileContent(ctx context.Context, fileID string) (*T.Object, error) {
	resp, err := g.service.Files.Get(fileID).Download()
	if err != nil {
		return nil, err
	}

	// if !keepOpen {
	// 	defer resp.Body.Close()
	// }

	obj := &T.Object{
		Body:          resp.Body,
		ContentType:   resp.Header.Get("Content-Type"),
		ContentLength: resp.ContentLength,
	}

	return obj, nil
}

func (g *GoogleDriveClient) DeleteFile(ctx context.Context, fileID string) error {
	err := g.service.Files.Delete(fileID).Do()
	if err != nil {
		return err
	}
	return nil
}

func (g *GoogleDriveClient) DownloadToFile(ctx context.Context, fileID string) (string, error) {
	resp, err := g.service.Files.Get(fileID).Download()
	if err != nil {
		return "", err

	}
	defer resp.Body.Close()

	file, err := g.service.Files.Get(fileID).Fields("name").Do()
	if err != nil {
		return "", err
	}

	zlogger.Logger.Info(fmt.Sprintf("Original File Name: %s", file.Name))
	destinationPath := path.Join(g.workDir, file.Name)

	out, err := os.Create(destinationPath)
	if err != nil {
		return "", err
	}

	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return "", err
	}

	zlogger.Logger.Info(fmt.Sprintf("Downloaded file ID: %s to %s", fileID, destinationPath))
	return destinationPath, nil
}

func (g *GoogleDriveClient) DownloadToMemory(ctx context.Context, fileID string, offset int64, chunkSize, fileSize int64) ([]byte, error) {
	limit := offset + chunkSize - 1
	if limit > fileSize {
		limit = fileSize
	}

	rng := fmt.Sprintf("bytes=%d-%d", offset, limit)

	req := g.service.Files.Get(fileID)

	req.Header().Set("Range", rng)

	resp, err := req.Download()
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data := make([]byte, chunkSize)
	n, err := io.ReadFull(resp.Body, data)

	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, err
	}

	if int64(n) < chunkSize && fileSize != chunkSize {
		data = data[:n]
	}

	return data, nil
}

func generateLargeFile(filename string, size int64) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	data := make([]byte, 10*1024*1024) // 1MB chunk
	for i := int64(0); i < size/(1024*1024); i++ {
		_, err := file.Write(data)
		if err != nil {
			return err
		}
	}

	return nil
}
