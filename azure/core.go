package azure

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"time"

	zlogger "github.com/0chain/s3migration/logger"
	T "github.com/0chain/s3migration/types"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
)

type AzureClient struct {
	service       *azblob.Client
	workDir       string
	newerThan     *time.Time
	olderThan     *time.Time
	containerName string
}

func NewAzureClient(workDir, accountName, connectionString, containerName string, newerThan *time.Time, olderThan *time.Time) (*AzureClient, error) {
	// Create the client
	blobURL := fmt.Sprintf("https://%s.blob.core.windows.net", accountName)
	zlogger.Logger.Info("bloburl", blobURL, accountName)

	client, err := azblob.NewClientFromConnectionString(connectionString, &azblob.ClientOptions{Audience: blobURL})
	if err != nil {
		return nil, fmt.Errorf("failed to create blob client: %v", err)
	}
	zlogger.Logger.Info("bloburl", blobURL)
	pager := client.NewListContainersPager(&azblob.ListContainersOptions{})

	_, err = pager.NextPage(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to validate client access: %v", err)
	}

	return &AzureClient{
		service:       client,
		workDir:       workDir,
		newerThan:     newerThan,
		olderThan:     olderThan,
		containerName: containerName,
	}, nil
}
func (g *AzureClient) ListFiles(ctx context.Context) (<-chan *T.ObjectMeta, <-chan error) {
	objectChan := make(chan *T.ObjectMeta)
	errChan := make(chan error)

	go func() {
		defer func() {
			close(objectChan)
			close(errChan)
		}()
		pager := g.service.NewListBlobsFlatPager(g.containerName, &azblob.ListBlobsFlatOptions{
			Include: azblob.ListBlobsInclude{Snapshots: true, Versions: true},
		})

		for pager.More() {
			fmt.Println("Pager has more data:", pager.More())
			resp, err := pager.NextPage(ctx)
			if err != nil {
				zlogger.Logger.Error("Error fetching page", err)
				errChan <- err
				return
			}

			for _, blob := range resp.Segment.BlobItems {
				fmt.Println(*blob.Name)
				lastModified := blob.Properties.LastModified
				if (g.newerThan == nil || g.newerThan.Unix() == 0 || lastModified.Unix() >= g.newerThan.Unix()) &&
					(g.olderThan == nil || g.olderThan.Unix() == 0 || lastModified.Unix() <= g.olderThan.Unix()) {
					fmt.Println("Pushing object to channel:", *blob.Name)
					objectChan <- &T.ObjectMeta{
						Key:         *blob.Name,
						Size:        *blob.Properties.ContentLength,
						ContentType: *blob.Properties.ContentType,
						Ext:         path.Ext(*blob.Name),
					}
				}
			}
		}

	}()

	return objectChan, errChan
}

func (g *AzureClient) GetFileContent(ctx context.Context, fileID string) (*T.Object, error) {
	resp, err := g.service.DownloadStream(ctx, g.containerName, fileID, nil)
	if err != nil {
		return nil, err
	}

	obj := &T.Object{
		Body:          resp.Body,
		ContentType:   *resp.ContentType,
		ContentLength: *resp.ContentLength,
	}

	return obj, nil
}

func (g *AzureClient) DeleteFile(ctx context.Context, fileID string) error {
	_, err := g.service.DeleteBlob(ctx, g.containerName, fileID, nil)
	if err != nil {
		return err
	}
	return nil
}

func (g *AzureClient) DownloadToFile(ctx context.Context, fileID string) (string, error) {
	resp, err := g.service.DownloadStream(ctx, g.containerName, fileID, nil)
	if err != nil {
		return "", err
	}

	zlogger.Logger.Info(fmt.Sprintf("Original File Name: %s", fileID))
	destinationPath := fileID

	out, err := os.Create(destinationPath)
	if err != nil {
		return "", err
	}

	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return "", err
	}

	zlogger.Logger.Info(fmt.Sprintf("Downloaded file ID: %s to %s\n", fileID, destinationPath))
	return destinationPath, nil
}

func (g *AzureClient) DownloadToMemory(ctx context.Context, fileID string, offset int64, chunkSize, fileSize int64) ([]byte, error) {
	limit := offset + chunkSize - 1
	if limit > fileSize {
		limit = fileSize
	}

	resp, err := g.service.DownloadStream(ctx, g.containerName, fileID, &azblob.DownloadStreamOptions{
		Range: azblob.HTTPRange{ // Pass by value, no `&` needed here
			Offset: offset,
			Count:  chunkSize,
		},
	})

	if err != nil {
		return nil, fmt.Errorf("failed to download chunk: %w", err)
	}
	defer resp.Body.Close()

	data := make([]byte, chunkSize)

	n, err := io.ReadFull(resp.Body, data)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, fmt.Errorf("error reading blob content: %w", err)
	}

	if int64(n) < chunkSize && fileSize != chunkSize {
		data = data[:n]
	}

	return data, nil
}
