package onedrive

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"

	zlogger "github.com/0chain/s3migration/logger"
	T "github.com/0chain/s3migration/types"

	// drive "github.com/goh-chunlin/go-onedrive/onedrive"
	drive "github.com/goh-chunlin/go-onedrive/onedrive"

	"github.com/pkg/errors"
	"golang.org/x/oauth2"
)

type OneDriveClient struct {
	client *drive.Client 
	workDir string
}

func NewOneDriveClient(token string, workDir string) (*OneDriveClient, error) {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)

	client := drive.NewClient(tc)
	_, err := client.Drives.List(ctx)
	
	if err != nil {
		return nil, errors.Wrap(err, "invalid Access token")
	}

	return &OneDriveClient{
		client: client,
		workDir: workDir,
	}, nil
}

func (g *OneDriveClient) ListFiles(ctx context.Context) (<-chan *T.ObjectMeta, <-chan error) {
	objectChan := make(chan *T.ObjectMeta)
	errChan := make(chan error)

	if g.client == nil {
	    errChan <- fmt.Errorf("client is not initialized")
    	return objectChan, errChan
	}

	go func() {
		defer func()  {
			close(objectChan)
			close(errChan)
		}()

		filesRes, err := g.client.DriveItems.List(ctx, "")

		if filesRes == nil {
			errChan <- fmt.Errorf("received nil response from List")
			return
		}

		if err != nil {
			errChan <- err
			return
		}
		for _, entry := range filesRes.DriveItems{
			 // Safely access the MIME type
			mimeType := "None"

			// Check if the File field is not nil
			if entry.File != nil {
				mimeType = entry.File.MIMEType
			}


			objectChan <- &T.ObjectMeta{
				Key: entry.Name,
				Size: entry.Size,
				ContentType: mimeType,
				Ext: filepath.Ext(entry.DownloadURL),
				Id: entry.Id,
			}
		}
	}()

  	go func() {
        for err := range errChan {
            fmt.Println("Error:", err) // Use a proper logging library if needed
        }
    }()

		return objectChan, errChan

	}


func (g *OneDriveClient) GetFileContent(ctx context.Context, fileID string) (*T.Object, error) {
	entry_item, err := g.client.DriveItems.Get(ctx, fileID)

	if err != nil {
		return nil, err
	}

	resp, err := http.Get(entry_item.DownloadURL)
    if err != nil {
        fmt.Println("Error making GET request:", err)
		return nil, err
    }
    defer resp.Body.Close()

	 if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return nil, errors.New(string(body))
    }

	localPath := entry_item.Name
    outFile, err := os.Create(localPath)
    if err != nil {
        return nil, fmt.Errorf("failed to create file: %w", err)
    }
    defer outFile.Close()
	
    _, err = io.Copy(outFile, resp.Body)
    if err != nil {
        return nil, fmt.Errorf("failed to write file: %w", err)
    }

    fmt.Println("File downloaded successfully:", localPath)
	
	fmt.Println(entry_item.Size)
    // Create the Object with the response data
    obj := &T.Object{
        Body:         resp.Body,
        ContentType:  resp.Header.Get("Content-Type"),
        ContentLength: entry_item.Size,
    }

	return obj, nil
}

func (g *OneDriveClient) DeleteFile(ctx context.Context, fileID string) error {
	driveId := ""
	_, err := g.client.DriveItems.Delete(ctx, driveId, fileID)

	if err != nil {
		return err
	}
	return nil
}


func (g *OneDriveClient) DownloadToFile(ctx context.Context, fileID string) (string, error) {
	entry_item, err := g.client.DriveItems.Get(ctx, fileID)

	if err != nil {
		return "", err
	}

	resp, err := http.Get(entry_item.DownloadURL)
    if err != nil {
        fmt.Println("Error making GET request:", err)
		return "", err
    }
    defer resp.Body.Close()

	 if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return "", errors.New(string(body))
    }

	destinationPath := path.Join(g.workDir, entry_item.Name )

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


func (g *OneDriveClient) DownloadToMemory(ctx context.Context, fileID string, offset int64, chunkSize, fileSize int64) ([]byte, error) {

	limit := offset + chunkSize - 1
	if limit > fileSize {
		limit = fileSize
	}

	req, err  := g.client.DriveItems.Get(ctx, fileID)

	if err != nil {
		return nil, err
	}

	apiURL := req.DownloadURL

	// Create a new HTTP request to download the chunk
	request, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	// Set the Range header for partial download
	rangeHeader := fmt.Sprintf("bytes=%d-%d", offset, limit)
	request.Header.Set("Range", rangeHeader)

	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("expected HTTP 206 Partial Content, got %d", resp.StatusCode)
	}

	// Allocate a buffer to read the chunk into memory
	data := make([]byte, chunkSize)
	n, err := io.ReadFull(resp.Body, data)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, err
	}

	// Adjust the size of the data if we didn't read the full chunk
	if int64(n) < chunkSize && fileSize != chunkSize {
		data = data[:n]
	}

	return data, nil
}

