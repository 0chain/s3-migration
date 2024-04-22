package dropbox

import (
	"context"
	"fmt"
	"testing"

	zlogger "github.com/0chain/s3migration/logger"
)

var (
	dropboxAccessToken = ""
	testFilePath       = ""
)

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

func TestDropboxClient_ListFiles(t *testing.T) {
	client, err := GetDropboxClient(dropboxAccessToken)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("Failed to create Dropbox client: %v", err))
		return
	}

	ctx := context.Background()
	objectChan, errChan := client.ListFiles(ctx)

	go func() {
		for err := range errChan {
			zlogger.Logger.Error(fmt.Sprintf("Error while listing files: %v", err))
		}
	}()

	for object := range objectChan {
		zlogger.Logger.Info(fmt.Sprintf("Key: %s, Type: %s, Size: %d bytes", object.Key, object.ContentType, object.Size))
	}
}

func TestDropboxClient_GetFileContent(t *testing.T) {
	client, err := GetDropboxClient(dropboxAccessToken)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("Failed to create Dropbox client: %v", err))
	}

	ctx := context.Background()
	filePath := testFilePath
	obj, err := client.GetFileContent(ctx, filePath)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("Error while getting file content: %v", err))
		return
	}
	defer obj.Body.Close()

	zlogger.Logger.Info(fmt.Sprintf("File content type: %s, Length: %d", obj.ContentType, obj.ContentLength))

	if (obj.Body == nil) || (obj.ContentLength == 0) {
		fmt.Println("Empty file content")
		return
	}

	buf := make([]byte, obj.ContentLength)
	n, err := obj.Body.Read(buf)

	if err != nil && err.Error() != "EOF" {
		zlogger.Logger.Error(fmt.Sprintf("Error while reading file content: %v", err))
		return
	}

	zlogger.Logger.Info(fmt.Sprintf("File content: %s", string(buf[:n])))
}

func TestDropboxClient_DeleteFile(t *testing.T) {
	client, err := GetDropboxClient(dropboxAccessToken)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("Failed to create Dropbox client: %v", err))
		return
	}

	ctx := context.Background()
	filePath := testFilePath
	err = client.DeleteFile(ctx, filePath)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("Error while deleting file: %v", err))
		return
	}
	zlogger.Logger.Info(fmt.Sprintf("File %s deleted successfully", filePath))
}

func TestDropboxClient_DownloadToFile(t *testing.T) {
	client, err := GetDropboxClient(dropboxAccessToken)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("Failed to create Dropbox client: %v", err))
		return
	}

	ctx := context.Background()
	filePath := testFilePath
	downloadedPath, err := client.DownloadToFile(ctx, filePath)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("Error while downloading file: %v", err))
		return
	}
	zlogger.Logger.Info(fmt.Sprintf("Downloaded to: %s", downloadedPath))
}

func TestDropboxClient_DownloadToMemory(t *testing.T) {
	client, err := GetDropboxClient(dropboxAccessToken)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("Failed to create Dropbox client: %v", err))
		return
	}

	ctx := context.Background()

	filePath := testFilePath
	offset := int64(0)

	// half chunk
	chunkSize := int64(313)
	objectSize := int64(626)

	data, err := client.DownloadToMemory(ctx, filePath, offset, chunkSize, objectSize)
	if err != nil {
		zlogger.Logger.Error(fmt.Sprintf("Error while downloading file: %v", err))
		return
	}

	zlogger.Logger.Info(fmt.Sprintf("Downloaded data: %s", data))
}
