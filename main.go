package main

import (
	"fmt"
	"os"

	"github.com/0chain/s3migration/cmd"
	_ "github.com/golang/mock/mockgen/model"
)

func main() {
	err := cmd.Execute()
	if err != nil {
		fmt.Println("Exiting migration due to error: ", err)
		os.Exit(1)
	}

	os.Exit(0)
}
