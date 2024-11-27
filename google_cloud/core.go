package gdrive

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"cloud.google.com/go/storage"
	zlogger "github.com/0chain/s3migration/logger"
	T "github.com/0chain/s3migration/types"
	"golang.org/x/oauth2"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

type GoogleCloudClient struct {
	service *storage.Client
	workDir string
}

func NewGoogleCloudClient(cfg oauth2.Config, token *oauth2.Token, workDir string) (*GoogleCloudClient, error) {

	ctx := context.Background()
	opts := []option.ClientOption{
		option.WithTokenSource(oauth2.StaticTokenSource(&oauth2.Token{
			AccessToken: token.AccessToken,
		})),
	}

	client, err := storage.NewClient(ctx, opts...)

	if err != nil {
		return nil, err
	}

	return &GoogleCloudClient{
		service: client,
		workDir: workDir,
	}, nil
}

func (g *GoogleCloudClient) ListFiles(ctx context.Context) (<-chan *T.ObjectMeta, <-chan error) {
	objectChan := make(chan *T.ObjectMeta)
	errChan := make(chan error)

	go func() {
		defer func() {
			close(objectChan)
			close(errChan)
		}()

		filesReq := g.service.Bucket(g.workDir).Objects(ctx, nil)

		for {
			attrs, err := filesReq.Next()
			if err == iterator.Done {
				break
			}

			if err != nil {
				errChan <- err
				return
			}
			objectChan <- &T.ObjectMeta{
				Key:         attrs.Name,
				Size:        attrs.Size,
				ContentType: attrs.ContentType,
				Ext:         path.Ext(attrs.Name),
			}
		}

	}()

	return objectChan, errChan
}

func (g *GoogleCloudClient) GetFileContent(ctx context.Context, fileID string) (*T.Object, error) {

	reader, err := g.service.Bucket(g.workDir).Object(fileID).NewReader(ctx)
	if err != nil {
		return nil, err
	}

	defer reader.Close()

	obj := &T.Object{
		Body:          reader,
		ContentType:   reader.Attrs.ContentType,
		ContentLength: reader.Attrs.Size,
	}

	return obj, nil
}

func (g *GoogleCloudClient) DeleteFile(ctx context.Context, fileID string) error {
	err := g.service.Bucket(g.workDir).Object(fileID).Delete(ctx)
	if err != nil {
		if strings.Contains(err.Error(), "storage: object doesn't exist") {
			zlogger.Logger.Error(fmt.Sprintf("File %s does not exist, skipping deletion.", fileID))
			return nil
		}
		zlogger.Logger.Error(fmt.Sprintf("Error while deleting file %s: %v", fileID, err))
		return err
	}
	zlogger.Logger.Error(fmt.Sprintf("File  %s deleted successfully", fileID))
	return nil
}

func (g *GoogleCloudClient) DownloadToFile(ctx context.Context, fileID string) (string, error) {
	object := g.service.Bucket(g.workDir).Object(fileID)

	resp, err := object.NewReader(ctx)
	if err != nil {
		return "", err
	}

	defer resp.Close()

	attrs, err := object.Attrs(ctx)

	zlogger.Logger.Info(fmt.Sprintf("Original File Name: %s", attrs.Name))
	destinationPath := path.Join(g.workDir, attrs.Name)

	out, err := os.Create(attrs.Name)
	if err != nil {
		return "", err
	}

	if _, err := io.Copy(out, resp); err != nil {
		return "", err
	}

	if err := out.Close(); err != nil {
		return "", err
	}
	zlogger.Logger.Info(fmt.Sprintf("Downloaded file ID: %s to %s\n", fileID, destinationPath))

	return destinationPath, nil
}

func (g *GoogleCloudClient) DownloadToMemory(ctx context.Context, fileID string, offset int64, chunkSize, fileSize int64) ([]byte, error) {
	limit := offset + chunkSize - 1
	if limit > fileSize {
		limit = fileSize
	}

	resp, err := g.service.Bucket(g.workDir).Object(fileID).NewReader(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to download chunk: %w", err)
	}
	defer resp.Close()

	data := make([]byte, chunkSize)

	n, err := io.ReadFull(resp, data)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, fmt.Errorf("error reading blob content: %w", err)
	}

	if int64(n) < chunkSize && fileSize != chunkSize {
		data = data[:n]
	}

	return data, nil
}
