package main

import (
	"github.com/0chain/s3migration/cmd"
	_ "github.com/golang/mock/mockgen/model"
)

func main() {
	err := cmd.Execute()
	if err != nil {
		panic(err)
	}
}
