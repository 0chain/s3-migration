package main

import (
	"github.com/0chain/s3migration/cmd"
	"github.com/google/martian/log"
)

func main() {
	err := cmd.Execute()
	if err != nil {
		log.Errorf("program encountered error: %v", err)
	}
}
