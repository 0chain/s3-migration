package box

import (
	"context"
	"fmt"
	"testing"

	zlogger "github.com/0chain/s3migration/logger"
)

const (
	testFileID = ""
)

var boxClient = BoxClient{
	ClientID:     "",
	ClientSecret: "",
	AccessToken:  "",
	RefreshToken: "",
}

// test cases
func TestBoxClient_ListFiles(t *testing.T) {
	ctx := context.Background()
	client, err := GetBoxClient(boxClient)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("err while creating Box client: %v", err))
		return
	}

	objectChan, errChan := client.ListFiles(ctx)
	go func() {
		for err := range errChan {
			zlogger.Logger.Error(fmt.Sprintf("err while list files: %v", err))
		}
	}()

	for object := range objectChan {
		zlogger.Logger.Info(fmt.Sprintf("file:%s, key: %s, size: %d bytes, type: %s", *object.Name, object.Key, object.Size, object.ContentType))
	}
}

func TestBoxClient_GetFileContent(t *testing.T) {
	ctx := context.Background()
	client, err := GetBoxClient(boxClient)

	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("Failed to creating Box client: %v", err))
		return
	}

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

func TestBoxClient_DownloadToFile(t *testing.T) {
	ctx := context.Background()
	client, err := GetBoxClient(boxClient)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("Failed to creating Box client: %v", err))
		return
	}

	fileID := testFileID

	destinationPath, err := client.DownloadToFile(ctx, fileID)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("err while downloading file: %v", err))
		return
	}
	zlogger.Logger.Info(fmt.Sprintf("downloaded to: %s", destinationPath))
}

func TestBoxClient_DownloadToMemory(t *testing.T) {
	ctx := context.Background()
	client, err := GetBoxClient(boxClient)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("Failed to creating Box client: %v", err))
		return
	}
	fileID := testFileID

	offset := int64(0)

	// download only half chunk for testing
	chunkSize := int64(313)

	fileSize := int64(626)

	data, err := client.DownloadToMemory(ctx, fileID, offset, chunkSize, fileSize)

	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("err while downloading file: %v", err))
		return
	}

	zlogger.Logger.Info(fmt.Sprintf("downloaded data: %s", data))
}

func TestBoxClient_DeleteFile(t *testing.T) {
	ctx := context.Background()
	client, err := GetBoxClient(boxClient)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("Failed to creating Box client: %v", err))
		return
	}
	fileID := testFileID

	err = client.DeleteFile(ctx, fileID)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("err while delete file: %v", err))
		return
	}
	zlogger.Logger.Info(fmt.Sprintf("file: %s deleted successfully", fileID))
}
