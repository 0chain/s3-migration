package gdrive

import (
	"context"
	"fmt"
	"testing"

	zlogger "github.com/0chain/s3migration/logger"
)

var (
	driveAccessToken = "ya29.a0Ad52N384rCQyODIBhljd7yg4RmP8czIUmIbJGkFHXNn-TnuEt3RL3ykzp0lEekSEoU0GDCkMnow31XOWFt0Dlw2l89fnO5sS8aiZKW8y0dDhqE6jtsNqpnA38EN5tBlRkNo0ipyY0Ps-cgX4hNxRRQcWhTZEqZgnOyLuaCgYKAWESARESFQHGX2Mi6ejaNEiKSZP6_H3hoXkeNA0171"
	testFileID       = ""
)

// using: https://developers.google.com/oauthplayground

// For reference (626 bytes text file)
const TestFileContent = ` by Manuel Gutiérrez Nájera

I want to die as the day declines, 
at high sea and facing the sky, 
while agony seems like a dream 
and my soul like a bird that can fly. 

To hear not, at this last moment, 
once alone with sky and sea, 
any more voices nor weeping prayers 
than the majestic beating of the waves. 

To die when the sad light retires 
its golden network from the green waves 
to be like the sun that slowly expires; 
something very luminous that fades. 

To die, and die young, before 
fleeting time removes the gentle crown, 
while life still says: "I'm yours" 
though we know with our hearts that she lies. 
`

func TestGoogleDriveClient_ListFiles(t *testing.T) {
	client, err := NewGoogleDriveClient(driveAccessToken, "./")
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
}

func TestGoogleDriveClient_GetFileContent(t *testing.T) {
	client, err := NewGoogleDriveClient(driveAccessToken, "./")
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

func TestGoogleDriveClient_DeleteFile(t *testing.T) {
	client, err := NewGoogleDriveClient(driveAccessToken, "./")
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

func TestGoogleDriveClient_DownloadToFile(t *testing.T) {
	client, err := NewGoogleDriveClient(driveAccessToken, "./")
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

func TestGoogleDriveClient_DownloadToMemory(t *testing.T) {
	client, err := NewGoogleDriveClient(driveAccessToken, "./")
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
