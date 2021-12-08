package controller

import (
	"context"
	"fmt"
	"github.com/0chain/gosdk/zboxcore/sdk"
	"github.com/0chain/s3migration/model"
	"github.com/0chain/s3migration/s3"
	s3svc "github.com/0chain/s3migration/s3/service"
	"log"
	"strings"
	"sync"
	"time"
)

const Batch = 10
const (
	Replace   = iota //Will replace existing file
	Skip             // Will skip migration if file already exists
	Duplicate        // Will add _copy prefix and uploads the file
)

var isMigrationInitialized bool

//Use context for all requests.
var rootContext context.Context
var rootContextCancel context.CancelFunc

var StateFilePath = func(homeDir, bucketName string) string {
	return fmt.Sprintf("%v/.aws/%v.state", homeDir, bucketName)
}

func abandonAllOperations() {
	rootContextCancel()
}

type bucket struct {
	name   string
	region string
	prefix string
}

type Migration struct {
	allocation *sdk.Allocation
	s3Service  s3.S3

	//Slice of map of bucket name and prefix. If prefix is empty string then every object will be uploaded.
	buckets      []bucket //{"bucket1": "prefix1"}
	bucketStates []map[string]*MigrationState

	resume bool
	skip   int

	//Number of goroutines to run. So at most concurrency * Batch goroutines will run. i.e. for bucket level and object level
	concurrency int
	encrypt     bool
}

func NewMigration() *Migration {
	return &Migration{}
}

func (m *Migration) InitMigration(ctx context.Context,allocation *sdk.Allocation, s3Service s3.S3, appConfig *model.AppConfig) error {
	m.s3Service = s3Service
	m.allocation = allocation
	m.resume = appConfig.Resume
	m.encrypt = appConfig.Encrypt
	m.skip = appConfig.Skip
	m.concurrency = appConfig.Concurrency

	if len(appConfig.Buckets) == 0 {
		// list all buckets form s3 and append them to m.buckets
		buckets, err := m.s3Service.ListAllBuckets(ctx)
		if err != nil {
			return err
		}

		bucketWithLocation, err := m.s3Service.GetBucketRegion(ctx, buckets)
		if err != nil {
			return err
		}

		for _, bkt := range bucketWithLocation {
			log.Println(bkt)
			// todo: get region for each bucket

			m.buckets = append(m.buckets, bucket{
				name:   bkt.Name,
				prefix: "",
				region: bkt.Location,
			})
		}
	} else {
		for _, bkt := range appConfig.Buckets {
			res := strings.Split(bkt, ":")
			l := len(res)
			if l < 1 || l > 2 {
				return fmt.Errorf("bucket flag has fields less than 1 or greater than 2. Arg \"%v\"", bkt)
			}

			bucketName := res[0]
			var prefix string

			if l == 2 {
				prefix = res[1]
			}

			bucketWithRegion, _ := m.s3Service.GetBucketRegion(ctx, []string{bucketName})
			if len(bucketWithRegion) == 0 {
				continue
			}
			m.buckets = append(m.buckets, bucket{
				name:   bucketName,
				prefix: prefix,
				region: bucketWithRegion[0].Location,
			})
		}
	}

	rootContext, rootContextCancel = context.WithCancel(ctx)

	isMigrationInitialized = true

	return nil
}

// getExistingFileList list existing files (with size) from dStorage
func setExistingFileList(allocationID string) error {
	dStorageFileList := make(map[string]model.FileRef, 0)
	allocationObj, err := sdk.GetAllocation(allocationID)
	if err != nil {
		log.Println("Error fetching the allocation", err)
		return err
	}
	// Create filter
	filter := []string{".DS_Store", ".git"}
	exclMap := make(map[string]int)
	for idx, path := range filter {
		exclMap[strings.TrimRight(path, "/")] = idx
	}

	remoteFiles, err := allocationObj.GetRemoteFileMap(exclMap)
	if err != nil {
		log.Println("Error getting remote files.", err)
		return err
	}

	// todo: add updated_by field in GetRemoteFileMap method of go-sdk

	for remoteFileName, remoteFileValue := range remoteFiles {
		if remoteFileValue.ActualSize > 0 {
			dStorageFileList[remoteFileName] = model.FileRef{Name: remoteFileName, Size: remoteFileValue.ActualSize}
			//dStorageFileList[remoteFileName] = model.FileRef{Name: remoteFileName, Size: remoteFileValue.ActualSize, ModifiedAt: remoteFileValue.UpdatedAt}
		}
	}

	s3svc.SetExistingFileList(dStorageFileList)

	return nil
}

func (m *Migration) Migrate() error {
	defer rootContextCancel()

	if !isMigrationInitialized {
		return fmt.Errorf("migration is not initialized")
	}

	if err := setExistingFileList(m.allocation.ID); err != nil {
		log.Println(err)
		return err
	}

	migrationFileQueue := make(chan model.FileRef)

	wg := sync.WaitGroup{}

	count := 0
	uploadInProgress := 0
	go func() {
		for {
			if uploadInProgress >= m.concurrency {
				continue
			}
			migrationFile := <-migrationFileQueue
			count++
			uploadInProgress++
			go func() {
				log.Println(migrationFile.Name, "will be uploaded from here")

				// todo:

				uploadInProgress--

				wg.Done()
			}()

			time.Sleep(time.Second)
		}
	}()

	for _, bkt := range m.buckets {
		_, err := m.s3Service.ListFilesInBucket(context.Background(), model.ListFileOptions{Bucket: bkt.name, Prefix: bkt.prefix, FileQueue: migrationFileQueue, WaitGroup: &wg})
		if err != nil {
			log.Println(err)
		}

	}
	fmt.Println("Waiting for all goroutine to complete")
	wg.Wait()
	fmt.Println("Waiting done", uploadInProgress)
	return nil
}

type objectUploadStatus struct {
	name       string
	isUploaded <-chan struct{}
	errCh      <-chan error
}

//MigrationState is state for each bucket.
type MigrationState struct {
	bucketName string

	uploadsInProgress [Batch]objectUploadStatus //Array that holds each objects status on successful upload or error

	//Migration is done in sorted order.
	//This key provides information that upto this key all the files are migrated.
	uptoKey string
}

func (ms *MigrationState) cleanBatch() {
	var errorNoticed bool
	for i := 0; i < Batch; i++ {
		oups := ms.uploadsInProgress[i]
		select {
		case <-oups.isUploaded:
			//Successfully uploaded
		case <-oups.errCh:
			errorNoticed = true
			//error occurred.
		}
	}

	if errorNoticed {
		//Stop migration from this bucket.
		//Save state in some file
	}
}
func (ms *MigrationState) saveState() {
	//Write its state to the file
	//Check objectUploadStatus
}
