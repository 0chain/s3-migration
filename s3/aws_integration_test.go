//go:build integration
// +build integration

package s3

import (
	"context"
	"log"
	"testing"
	"time"

	"github.com/0chain/s3migration/util"
)

func TestService_ListAllBuckets(t *testing.T) {
	awsAccessKey := ""
	awsSecretKey := ""

	util.SetAwsEnvCredentials(awsAccessKey, awsSecretKey)
	s3Svc, _ := GetAwsClient("", "", "", false, nil, nil, "", "")
	fileListChan, errChan := s3Svc.ListFiles(context.Background())
	for {
		objKey, ok := <-fileListChan
		if ok {
			log.Println(objKey)
		} else {
			break
		}
	}

	if err, ok := <-errChan; ok && err != nil {
		log.Println(err)
	}
}

func TestAwsClient_GetFileContent(t *testing.T) {
	awsAccessKey := ""
	awsSecretKey := ""

	util.SetAwsEnvCredentials(awsAccessKey, awsSecretKey)

	objectKey := ""
	s3Svc, _ := GetAwsClient("", "", "", false, nil, nil, "", "")
	x, err := s3Svc.GetFileContent(context.Background(), objectKey)
	if err != nil {
		log.Println(err)
		return
	}

	for {
		d := make([]byte, 10000)
		_, err := x.Body.Read(d)
		if err != nil {
			break
		}
		log.Println(string(d))
	}

}

func TestService_GetBucketRegion(t *testing.T) {
	awsAccessKey := ""
	awsSecretKey := ""

	util.SetAwsEnvCredentials(awsAccessKey, awsSecretKey)

	s3Svc, _ := GetAwsClient("", "", "", false, nil, nil, "", "")

	bucketData, err := s3Svc.getBucketRegion()
	if err != nil {
		t.Fatalf("got error while fetching location data, err = %+v", err)
	}
	log.Printf("bucket list, %+v, err = %+v", bucketData, err)
}

func TestService_DeleteFile(t *testing.T) {
	awsAccessKey := ""
	awsSecretKey := ""

	util.SetAwsEnvCredentials(awsAccessKey, awsSecretKey)

	objectKey := ""
	s3Svc, _ := GetAwsClient("", "", "", false, nil, nil, "", "")
	err := s3Svc.DeleteFile(context.Background(), objectKey)
	if err != nil {
		log.Printf("object key deletion error,err = %+v", err)
	}
}

func TestAwsClient_DownloadManager(t *testing.T) {
	awsAccessKey := "AKIA4MPQDEZ4FKNA3OPR"
	awsSecretKey := "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"

	util.SetAwsEnvCredentials(awsAccessKey, awsSecretKey)
	objectKey := "filesforbucket/65.txt"
	x := time.Now()
	s3Svc, _ := GetAwsClient("lpobkt1", "", "", false, nil, nil, "", "/Users/mdmiranahmedansari/aws_temp")
	downloadPath, err := s3Svc.DownloadToFile(context.Background(), objectKey)
	log.Println(downloadPath, err)
	log.Println(time.Now().Sub(x).Seconds())
}
