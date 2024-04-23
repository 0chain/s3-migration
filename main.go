package main

import (
	"testing"

	"github.com/0chain/s3migration/dropbox"
	_ "github.com/golang/mock/mockgen/model"
)

func main() {
	t := testing.T{}
	dropbox.TestDropboxClient_ListFiles(&t)

	// err := cmd.Execute()
	// if err != nil {
	// fmt.Println("Exiting migration due to error: ", err)
	// os.Exit(1)
	// }

	// os.Exit(0)
}
