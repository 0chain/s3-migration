package gdrive

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	zlogger "github.com/0chain/s3migration/logger"
	T "github.com/0chain/s3migration/types"
	"github.com/pkg/errors"
	"golang.org/x/oauth2"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

type GoogleDriveClient struct {
	service   *drive.Service
	workDir   string
	newerThan *time.Time
	olderThan *time.Time
}

func NewGoogleDriveClient(cfg oauth2.Config, token *oauth2.Token, workDir string, newerThan *time.Time, olderThan *time.Time) (*GoogleDriveClient, error) {
	ctx := context.Background()
	var httpClient *http.Client

	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		tokenSource := oauth2.StaticTokenSource(&oauth2.Token{
			AccessToken:  token.AccessToken,
			RefreshToken: token.RefreshToken,
		})
		httpClient = oauth2.NewClient(ctx, tokenSource)
	} else {
		httpClient = cfg.Client(ctx, token)
	}

	// if access token is expired, refresh it
	if token.Expiry.Before(time.Now()) {
		token, err := cfg.TokenSource(ctx, token).Token()
		if err != nil {
			return nil, err
		}
		tokenSource := oauth2.StaticTokenSource(&oauth2.Token{
			AccessToken:  token.AccessToken,
			RefreshToken: token.RefreshToken,
		})
		httpClient = oauth2.NewClient(ctx, tokenSource)
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
		service:   service,
		workDir:   workDir,
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
					Key:         file.Id,
					Size:        file.Size,
					ContentType: file.MimeType,
					Ext:         file.FileExtension,
					Name:        &file.Name,
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
					Key:         file.Id,
					Size:        file.Size,
					ContentType: file.MimeType,
					Ext:         file.FileExtension,
					Name:        &file.Name,
				}
			}

			nextPgToken = files.NextPageToken
		}
	}()

	return objectChan, errChan
}

func (g *GoogleDriveClient) GetFileContent(ctx context.Context, fileID string) (*T.Object, error) {
	// Get file metadata first to check if it needs to be exported
	file, err := g.service.Files.Get(fileID).Fields("mimeType").Do()
	if err != nil {
		return nil, err
	}

	var resp *http.Response
	// Handle Google Workspace files that need to be exported
	switch file.MimeType {
	case "application/vnd.google-apps.document":
		resp, err = g.service.Files.Export(fileID, "application/vnd.openxmlformats-officedocument.wordprocessingml.document").Download()
	case "application/vnd.google-apps.spreadsheet":
		resp, err = g.service.Files.Export(fileID, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet").Download()
	case "application/vnd.google-apps.presentation":
		resp, err = g.service.Files.Export(fileID, "application/vnd.openxmlformats-officedocument.presentationml.presentation").Download()
	default:
		// Regular file download
		resp, err = g.service.Files.Get(fileID).Download()
	}

	if err != nil {
		return nil, err
	}

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
	// Get file metadata first
	file, err := g.service.Files.Get(fileID).Fields("name, mimeType").Do()
	if err != nil {
		return "", err
	}

	var resp *http.Response
	fileName := file.Name

	// Handle Google Workspace files that need to be exported
	switch file.MimeType {
	case "application/vnd.google-apps.document":
		resp, err = g.service.Files.Export(fileID, "application/vnd.openxmlformats-officedocument.wordprocessingml.document").Download()
		if !strings.HasSuffix(fileName, ".docx") {
			fileName += ".docx"
		}
	case "application/vnd.google-apps.spreadsheet":
		resp, err = g.service.Files.Export(fileID, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet").Download()
		if !strings.HasSuffix(fileName, ".xlsx") {
			fileName += ".xlsx"
		}
	case "application/vnd.google-apps.presentation":
		resp, err = g.service.Files.Export(fileID, "application/vnd.openxmlformats-officedocument.presentationml.presentation").Download()
		if !strings.HasSuffix(fileName, ".pptx") {
			fileName += ".pptx"
		}
	default:
		// Regular file download
		resp, err = g.service.Files.Get(fileID).Download()
	}

	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	zlogger.Logger.Info(fmt.Sprintf("Original File Name: %s", fileName))
	destinationPath := path.Join(g.workDir, fileName)

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
