package onedrive

import (
	"context"
	"fmt"
	"testing"

	zlogger "github.com/0chain/s3migration/logger"
	"golang.org/x/oauth2"
)

var (
	access     = "access"
	refresh    = "refresh"
	testFileID = "file_id"
	token      = &oauth2.Token{
		AccessToken:  access,
		RefreshToken: refresh,
	}
)

func TestOneDriveClient_ListFiles(t *testing.T) {
	client, err := NewOneDriveClient(token, "./", nil, nil)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("err while creating One Drive client: %v", err))
		return
	}

	ctx := context.Background()
	objectChan, errChan := client.ListFiles(ctx)

	go func() {
		for err := range errChan {
			zlogger.Logger.Error(fmt.Sprintf("err while list files: %v", err))
		}
	}()

	for object := range objectChan {
		zlogger.Logger.Info(fmt.Sprintf("file:%s, size: %d bytes, type: %s, id: %s", object.Key, object.Size, object.ContentType, *object.Id))
	}
}

func TestOneDriveClient_GetFileContent(t *testing.T) {
	client, err := NewOneDriveClient(token, "./", nil, nil)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("Error while creating OneDrive client: %v", err))
		return
	}

	ctx := context.Background()
	obj, err := client.GetFileContent(ctx, testFileID)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("Error while getting file content: %v", err))
		return
	}
	defer func() {
		if closeErr := obj.Body.Close(); closeErr != nil {
			zlogger.Logger.Error(fmt.Sprintf("Error closing response body: %v", closeErr))
		}
	}()

	if obj.Body == nil || obj.ContentLength <= 0 {
		zlogger.Logger.Info("Empty file content")
		return
	}

}

func TestOneDriveDeleteFile(t *testing.T) {
	client, err := NewOneDriveClient(token, "./", nil, nil)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("Error while creating OneDrive client: %v", err))
		return
	}

	ctx := context.Background()
	err = client.DeleteFile(ctx, testFileID)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("Error while deleting file: %v", err))
		return
	}
	zlogger.Logger.Info("File deleted successfully")
}

func TestOneDriveDownloadFile(t *testing.T) {
	client, err := NewOneDriveClient(token, "./", nil, nil)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("Error while creating OneDrive client: %v", err))
		return
	}

	ctx := context.Background()
	downloadedPath, err := client.DownloadToFile(ctx, testFileID)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("Error while downloading file: %v", err))
		return
	}
	zlogger.Logger.Info(fmt.Sprintf("Downloaded to: %s", downloadedPath))
}

func TestOneDriveDownloadToMemory(t *testing.T) {
	client, err := NewOneDriveClient(token, "./", nil, nil)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("Error while creating OneDrive client: %v", err))
		return
	}

	ctx := context.Background()

	fileID := testFileID
	offset := int64(0)
	chunkSize := int64(313)
	objectSize := int64(626)

	data, err := client.DownloadToMemory(ctx, fileID, offset, chunkSize, objectSize)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("Error while downloading file: %v", err))
		return
	}
	zlogger.Logger.Info(fmt.Sprintf("Downloaded data: %s", data))
}
