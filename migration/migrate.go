package migration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	dStorage "github.com/0chain/s3migration/dstorage"
	zlogger "github.com/0chain/s3migration/logger"
	"github.com/0chain/s3migration/s3"
	"github.com/0chain/s3migration/util"
	zerror "github.com/0chain/s3migration/zErrors"
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

var StateFilePath = func(workDir, bucketName string) string {
	return fmt.Sprintf("%v/%v.state", workDir, bucketName)
}

func abandonAllOperations(err error) {
	if err != nil {
		zlogger.Logger.Error(err)
	}
	rootContextCancel()
}

type Migration struct {
	zStore   dStorage.DStoreI
	awsStore s3.AwsI

	skip       int
	retryCount int

	//Number of goroutines to run. So at most concurrency * Batch goroutines will run. i.e. for bucket level and object level
	concurrency int

	szCtMu               sync.Mutex //size and count mutex; used to update migratedSize and totalMigratedObjects
	migratedSize         uint64
	totalMigratedObjects uint64

	stateFilePath string
	migrateTo     string
	workDir       string
	deleteSource  bool
}

func InitMigration(mConfig *MigrationConfig) error {
	zlogger.Logger.Info("Initializing migration")
	zlogger.Logger.Info("Getting dStorage service")
	dStorageService, err := dStorage.GetDStorageService(
		mConfig.AllocationID,
		mConfig.MigrateToPath,
		mConfig.DuplicateSuffix,
		mConfig.WorkDir,
		mConfig.Encrypt,
		mConfig.WhoPays,
	)
	if err != nil {
		zlogger.Logger.Error(err)
		return err
	}

	zlogger.Logger.Info("Getting aws storage service")
	awsStorageService, err := s3.GetAwsClient(
		mConfig.Bucket,
		mConfig.Prefix,
		mConfig.Region,
		mConfig.DeleteSource,
		mConfig.NewerThan,
		mConfig.OlderThan,
		mConfig.StartAfter,
		mConfig.WorkDir,
	)
	if err != nil {
		zlogger.Logger.Error(err)
		return err
	}

	migration = Migration{
		zStore:        dStorageService,
		awsStore:      awsStorageService,
		skip:          mConfig.Skip,
		concurrency:   mConfig.Concurrency,
		retryCount:    mConfig.RetryCount,
		stateFilePath: mConfig.StateFilePath,
		migrateTo:     mConfig.MigrateToPath,
		deleteSource:  mConfig.DeleteSource,
		workDir:       mConfig.WorkDir,
	}

	rootContext, rootContextCancel = context.WithCancel(context.Background())

	trapCh := util.SignalTrap(os.Interrupt, os.Kill, syscall.SIGTERM)

	go func() {
		sig := <-trapCh
		zlogger.Logger.Info(fmt.Sprintf("Signal %v received", sig))
		abandonAllOperations(errors.New("operation cancelled by user"))
	}()

	isMigrationInitialized = true

	return nil
}

type migratingObjStatus struct {
	objectKey string
	successCh chan struct{}
	errCh     chan error //should be of type zerror
}

func processMigrationBatch(objList []*s3.ObjectMeta, migrationStatuses []*migratingObjStatus, count int) (stateKey string, batchProcessSuccess bool) {
	migration.zStore.UpdateAllocationDetails()
	availableStorage := migration.zStore.GetAvailableSpace()

	var batchStorageSize int64
	for i := 0; i < count; i++ {
		obj := objList[i]
		batchStorageSize += obj.Size
	}

	if availableStorage < batchStorageSize {
		zlogger.Logger.Info(fmt.Sprintf("Insufficient Space available space: %v, batchStorageSpace: %v", availableStorage, batchStorageSize))
		abandonAllOperations(zerror.ErrInsufficientSpace)
		return
	}

	wg := sync.WaitGroup{}
	for i := 0; i < count; i++ {
		obj := objList[i]
		zlogger.Logger.Info("Migrating ", obj.Key)
		wg.Add(1)
		status := migrationStatuses[i]
		status.objectKey = obj.Key
		status.successCh = make(chan struct{}, 1)
		status.errCh = make(chan error, 1)
		go migrateObject(&wg, obj, status, rootContext)
	}
	wg.Wait()

	stateKey, unresolvedError := checkStatuses(migrationStatuses[:count])

	if unresolvedError != nil {
		//break migration
		abandonAllOperations(unresolvedError)
		return
	}
	batchProcessSuccess = true
	return
}

func Migrate() error {
	defer rootContextCancel()

	if !isMigrationInitialized {
		return fmt.Errorf("migration is not initialized")
	}

	updateState, closeStateFile, err := updateStateKeyFunc(migration.stateFilePath)
	if err != nil {
		return fmt.Errorf("could not create state file path. Error: %v", err)
	}
	defer closeStateFile()

	objCh, errCh := migration.awsStore.ListFilesInBucket(rootContext)

	count := 0
	batchCount := 0
	migrationStatuses := make([]*migratingObjStatus, 10)
	objectMetaList := make([]*s3.ObjectMeta, 10)
	makeMigrationStatuses := func() {
		for i := 0; i < 10; i++ {
			migrationStatuses[i] = new(migratingObjStatus)
		}
	}

	makeMigrationStatuses()
	for obj := range objCh {
		objectMetaList[count] = obj
		count++
		if count == 10 {
			batchCount++
			stateKey, batchProcessSuccess := processMigrationBatch(objectMetaList, migrationStatuses, count)
			if !batchProcessSuccess {
				count = 0
				break
			}
			count = 0
			//log statekey
			updateState(stateKey)
			time.Sleep(100 * time.Millisecond)
		}
	}

	if count != 0 { //last batch that is not multiple of 10
		batchCount++
		stateKey, batchProcessSuccess := processMigrationBatch(objectMetaList, migrationStatuses, count)
		if batchProcessSuccess {
			updateState(stateKey)
		}

	}

	zlogger.Logger.Info("Total migrated objects: ", migration.totalMigratedObjects)
	zlogger.Logger.Info("Total migrated size: ", migration.migratedSize)

	select {
	case err := <-errCh:
		if err != nil {
			zlogger.Logger.Error("Could not fetch all objects. Error: ", err)
		} else {
			zlogger.Logger.Info("Got object from s3 without error")
		}
	case <-rootContext.Done():
		zlogger.Logger.Error("Error: context cancelled")
	}

	return nil
}

func checkStatuses(statuses []*migratingObjStatus) (stateKey string, unresolvedError error) {
	for _, mgrtStatus := range statuses {
		select {
		case <-mgrtStatus.successCh:
			stateKey = mgrtStatus.objectKey

		case err := <-mgrtStatus.errCh:
			unresolvedError = err
			if resolveError(mgrtStatus.objectKey, err) {
				stateKey = mgrtStatus.objectKey
				unresolvedError = nil
			} else {
				return
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

func updateStateKeyFunc(statePath string) (func(stateKey string), func(), error) {
	f, err := os.Create(statePath)
	if err != nil {
		return nil, nil, err
	}
	var errorWhileWriting bool
	stateKeyUpdater := func(stateKey string) {
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
	}

	fileCloser := func() { f.Close() }

	return stateKeyUpdater, fileCloser, nil
}

func migrateObject(wg *sync.WaitGroup, objMeta *s3.ObjectMeta, status *migratingObjStatus, ctx context.Context) {
	defer wg.Done()

	remotePath := filepath.Join(migration.migrateTo, objMeta.Key)

	isFileExist, err := migration.zStore.IsFileExist(ctx, remotePath)

	if err != nil {
		zlogger.Logger.Error(err)
		status.errCh <- err
		return
	}

	if isFileExist && migration.skip == Skip {
		zlogger.Logger.Info("Skipping migration of object" + objMeta.Key)
		status.successCh <- struct{}{}
		return
	}

	obj, err := migration.awsStore.GetFileContent(ctx, objMeta.Key)
	if err != nil {
		zlogger.Logger.Error(err)
		status.errCh <- err
		return
	}

	if isFileExist {
		switch migration.skip {
		case Replace:
			zlogger.Logger.Info("Replacing object" + objMeta.Key + " size " + strconv.FormatInt(objMeta.Size, 10))
			err = migration.zStore.Replace(ctx, remotePath, obj.Body, objMeta.Size, obj.ContentType)
		case Duplicate:
			zlogger.Logger.Info("Duplicating object" + objMeta.Key + " size " + strconv.FormatInt(objMeta.Size, 10))
			err = migration.zStore.Duplicate(ctx, remotePath, obj.Body, objMeta.Size, obj.ContentType)
		}
	} else {
		zlogger.Logger.Info("Uploading object" + objMeta.Key + " size " + strconv.FormatInt(objMeta.Size, 10))
		err = migration.zStore.Upload(ctx, remotePath, obj.Body, objMeta.Size, obj.ContentType, false)
	}

	if err != nil {
		zlogger.Logger.Error(err)
		status.errCh <- err
	} else {
		status.successCh <- struct{}{}
		migration.szCtMu.Lock()
		migration.migratedSize += uint64(objMeta.Size)
		migration.totalMigratedObjects++
		migration.szCtMu.Unlock()

		if migration.deleteSource {
			migration.awsStore.DeleteFile(ctx, objMeta.Key)
		}
	}
}
