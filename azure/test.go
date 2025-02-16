package azure

import (
	"context"
	"fmt"
	"testing"

	zlogger "github.com/0chain/s3migration/logger"
)

var (
	connectionString = ""
	testFileID       = ""
	workDir          = ""
	accountName      = ""
)

func TestAzureClient_ListFiles(t *testing.T) {
	client, err := NewAzureClient(workDir, accountName, connectionString, workDir, nil, nil)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("err while creating Google Drive client: %v", err))
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
		zlogger.Logger.Info(fmt.Sprintf("file:%s, size: %d bytes, type: %s", object.Key, object.Size, object.ContentType))
	}
	zlogger.Logger.Info("DATA")
}

func TestAzureClient_GetFileContent(t *testing.T) {
	client, err := NewAzureClient(workDir, accountName, connectionString, workDir, nil, nil)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("Failed to creating Google Drive client: %v", err))
		return
	}

	ctx := context.Background()
	fileID := testFileID
	obj, err := client.GetFileContent(ctx, fileID)

	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("err while getting file content: %v", err))
		return
	}

	defer obj.Body.Close()

	zlogger.Logger.Info(fmt.Sprintf("file content type: %s, length: %d", obj.ContentType, obj.ContentLength))

	if (obj.Body == nil) || (obj.ContentLength == 0) {
		zlogger.Logger.Info("empty file content")
		return
	}

	buf := make([]byte, obj.ContentLength)
	n, err := obj.Body.Read(buf)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("err while read file content: %v", err))
		return
	}
	zlogger.Logger.Info(fmt.Sprintf("read data: %s", buf[:n]))
}

func TestAzureClient_DeleteFile(t *testing.T) {
	client, err := NewAzureClient(workDir, accountName, connectionString, workDir, nil, nil)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("err while creating Google Drive client: %v", err))
		return
	}

	ctx := context.Background()
	fileID := testFileID
	err = client.DeleteFile(ctx, fileID)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("err while delete file: %v", err))
		return
	}
	zlogger.Logger.Error(fmt.Sprintf("file: %s deleted successfully", fileID))
}

func TestAzureClient_DownloadToFile(t *testing.T) {
	client, err := NewAzureClient(workDir, accountName, connectionString, workDir, nil, nil)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("err while creating Google Drive client: %v", err))
	}

	ctx := context.Background()
	fileID := testFileID
	destinationPath, err := client.DownloadToFile(ctx, fileID)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("err while downloading file: %v", err))
		return
	}
	zlogger.Logger.Info(fmt.Sprintf("downloaded to: %s", destinationPath))
}

func TestAzureClient_DownloadToMemory(t *testing.T) {
	client, err := NewAzureClient(workDir, accountName, connectionString, workDir, nil, nil)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("err while creating Google Drive client: %v", err))
	}

	ctx := context.Background()

	fileID := testFileID

	offset := int64(0)

	// download only half chunk for testing
	chunkSize := int64(313)

	fileSize := int64(626)

	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("err while getting file size: %v", err))
		return
	}

	data, err := client.DownloadToMemory(ctx, fileID, offset, chunkSize, fileSize)

	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("err while downloading file: %v", err))
		return
	}

	zlogger.Logger.Info(fmt.Sprintf("downloaded data: %s", data))
}
