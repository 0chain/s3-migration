package migration

import (
	"context"
	"fmt"
	"os"
	"sync"

	dStorage "github.com/0chain/s3migration/dstorage"
	"github.com/0chain/s3migration/s3"
)

const Batch = 10
const (
	Replace   = iota //Will replace existing file
	Skip             // Will skip migration if file already exists
	Duplicate        // Will add _copy prefix and uploads the file
)

var migration Migration
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

type Migration struct {
	zStore   dStorage.DStoreI
	awsStore s3.AwsI

	//Slice of map of bucket name and prefix. If prefix is empty string then every object will be uploaded.
	//If buckets is empty; migrate all buckets
	bucketStates []map[string]*MigrationState

	skip       int
	retryCount int

	//Number of goroutines to run. So at most concurrency * Batch goroutines will run. i.e. for bucket level and object level
	concurrency int

	migratedSize uint64

	stateFilePath string
}

func InitMigration(mConfig *MigrationConfig) error {
	dStorageService, err := dStorage.GetDStorageService(mConfig.AllocationID, mConfig.MigrateToPath, mConfig.Encrypt, mConfig.WhoPays)
	if err != nil {
		return err
	}

	awsStorageService, err := s3.GetAwsClient(
		mConfig.Bucket,
		mConfig.Prefix,
		mConfig.Region,
		mConfig.DeleteSource,
		mConfig.NewerThan,
		mConfig.OlderThan,
		mConfig.StartAfter,
	)
	if err != nil {
		return err
	}

	migration = Migration{
		zStore:        dStorageService,
		awsStore:      awsStorageService,
		skip:          mConfig.Skip,
		concurrency:   mConfig.Concurrency,
		retryCount:    mConfig.RetryCount,
		stateFilePath: mConfig.StateFilePath,
	}

	rootContext, rootContextCancel = context.WithCancel(context.Background())

	isMigrationInitialized = true

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

type migratingObjStatus struct {
	objectKey string
	successCh chan struct{}
	errCh     chan error
}

func Migrate() error {
	defer rootContextCancel()

	if !isMigrationInitialized {
		return fmt.Errorf("migration is not initialized")
	}

	updateState, err := updateStateKey(migration.stateFilePath)
	if err != nil {
		return fmt.Errorf("could not create state file path. Error: %v", err)
	}
	objCh, _ := migration.awsStore.ListFilesInBucket(rootContext)
	// if err != nil {
	// 	return err
	// }

	count := 0
	batchCount := 1
	wg := sync.WaitGroup{}

	migrationStatuses := make([]*migratingObjStatus, 10)
	for _, ms := range migrationStatuses {
		ms.successCh = make(chan struct{}, 1)
		ms.errCh = make(chan error, 1)
	}

	for obj := range objCh {
		status := migrationStatuses[count]
		wg.Add(1)

		go func(status *migratingObjStatus, obj interface{}) {
			defer wg.Done()
		}(status, obj)

		count++

		if count == 10 {
			batchCount++

			wg.Wait()

			stateKey, unresolvedError := checkStatuses(migrationStatuses)

			if unresolvedError {
				//break migration
				abandonAllOperations()
			}

			//log statekey
			updateState(stateKey)
			count = 0
		}

	}
	wg.Wait()
	return nil
}

func checkStatuses(statuses []*migratingObjStatus) (stateKey string, unresolvedError bool) {
	for _, mgrtStatus := range statuses {
		select {
		case <-mgrtStatus.successCh:
			stateKey = mgrtStatus.objectKey

		case err := <-mgrtStatus.errCh:
			unresolvedError = true
			if resolveError(mgrtStatus.objectKey, err) {
				stateKey = mgrtStatus.objectKey
				unresolvedError = false
			} else {
				break
			}
		}
	}

	return
}

func resolveError(objectKey string, err error) (isErrorResolved bool) {
	switch err.(type) {

	}

	return
}

func updateStateKey(statePath string) (func(stateKey string), error) {
	f, err := os.Create(statePath)
	if err != nil {
		return nil, err
	}
	var errorWhileWriting bool
	return func(stateKey string) {
		if errorWhileWriting {
			f, err = os.Create(statePath)
			if err != nil {
				return
			}
			_, err = f.Write([]byte(stateKey))
			if err != nil {
				return
			}
			errorWhileWriting = false
		}

		err = f.Truncate(0)
		if err != nil {
			errorWhileWriting = true
			return
		}
		_, err = f.Seek(0, 0)
		if err != nil {
			errorWhileWriting = true
			return
		}

		_, err = f.Write([]byte(stateKey))
		if err != nil {
			errorWhileWriting = true
		}
	}, nil
}
