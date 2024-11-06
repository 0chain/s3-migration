package main

import (
	"os"
	"testing"

	onedrive "github.com/0chain/s3migration/onedrive"
	_ "github.com/golang/mock/mockgen/model"
)

func main() {
	t := &testing.T{}
	// onedrive.TestOneDriveClient_ListFiles(t)
	onedrive.TestOneDriveClient_GetFileContent(t)
	os.Exit(0)
}
