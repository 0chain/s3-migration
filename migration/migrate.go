package migration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/0chain/gosdk/zboxcore/zboxutil"
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

const (
	uploadCountFileName = "upload.count"
)

var migration Migration

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
	fs       util.FileSystem

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
	bucket        string
}

func updateTotalObjects(awsStorageService *s3.AwsClient, wd string) error {
	f, err := os.Create(filepath.Join(wd, "files.total"))
	if err != nil {
		return err
	}
	defer f.Close()

	var totalFiles int
	objCh, errCh := awsStorageService.ListFilesInBucket(context.Background())

L1:
	for {
		select {
		case _, ok := <-objCh:
			if !ok {
				break L1
			}
			totalFiles++
		case err = <-errCh:
			if err != nil {
				return err
			}
		}
	}

	_, err = f.WriteString(strconv.Itoa(totalFiles))
	return err
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

	err = updateTotalObjects(awsStorageService, mConfig.WorkDir)
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
		bucket:        mConfig.Bucket,
		fs:            util.Fs,
	}

	rootContext, rootContextCancel = context.WithCancel(context.Background())

	trapCh := util.SignalTrap(os.Interrupt, os.Kill, syscall.SIGTERM)

	go func() {
		sig := <-trapCh
		zlogger.Logger.Info(fmt.Sprintf("Signal %v received", sig))
		abandonAllOperations(zerror.ErrOperationCancelledByUser)
	}()

	return nil
}

var updateKeyFunc = func(statePath string) (func(stateKey string), func(), error) {
	f, err := os.Create(statePath)
	if err != nil {
		return nil, nil, err
	}
	var errorWhileWriting bool
	keyUpdater := func(key string) {
		if errorWhileWriting {
			f, err = os.Create(statePath)
			if err != nil {
				return
			}
			_, err = f.Write([]byte(key))
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

		_, err = f.Write([]byte(key))
		if err != nil {
			errorWhileWriting = true
		}
	}

	fileCloser := func() { f.Close() }

	return keyUpdater, fileCloser, nil
}

func StartMigration() error {
	defer func(start time.Time) {
		zlogger.Logger.Info("time taken: ", time.Since(start))
	}(time.Now())

	migrationWorker := NewMigrationWorker(migration.workDir)
	go migration.DownloadWorker(rootContext, migrationWorker)
	go migration.UploadWorker(rootContext, migrationWorker)
	migration.UpdateStateFile(migrationWorker)
	err := migrationWorker.GetMigrationError()
	if err != nil {
		zlogger.Logger.Error("Error while migration, err", err)
	}
	zlogger.Logger.Info("Total migrated objects: ", migration.totalMigratedObjects)
	zlogger.Logger.Info("Total migrated size: ", migration.migratedSize)
	return err
}

func (m *Migration) DownloadWorker(ctx context.Context, migrator *MigrationWorker) {
	defer migrator.CloseDownloadQueue()
	objCh, errCh := migration.awsStore.ListFilesInBucket(rootContext)
	wg := &sync.WaitGroup{}
	for obj := range objCh {
		zlogger.Logger.Info("Downloading object: ", obj.Key)
		migrator.PauseDownload()
		if migrator.IsMigrationError() {
			return
		}
		wg.Add(1)

		downloadObjMeta := &DownloadObjectMeta{
			ObjectKey: obj.Key,
			Size:      obj.Size,
			DoneChan:  make(chan struct{}, 1),
			ErrChan:   make(chan error, 1),
		}

		go func() {
			defer wg.Done()
			err := checkIsFileExist(ctx, downloadObjMeta)
			if err != nil {
				zlogger.Logger.Error(err)
				migrator.SetMigrationError(err)
				return
			}
			if downloadObjMeta.IsFileAlreadyExist && migration.skip == Skip {
				zlogger.Logger.Info("Skipping migration of object" + downloadObjMeta.ObjectKey)
				migrator.DownloadStart(downloadObjMeta)
				migrator.DownloadDone(downloadObjMeta, "", nil)
				return
			}
			migrator.DownloadStart(downloadObjMeta)
			zlogger.Logger.Info("download start", downloadObjMeta.ObjectKey, downloadObjMeta.Size)
			downloadPath, err := m.awsStore.DownloadToFile(ctx, downloadObjMeta.ObjectKey)
			migrator.DownloadDone(downloadObjMeta, downloadPath, err)
			migrator.SetMigrationError(err)
			zlogger.Logger.Info("download done", downloadObjMeta.ObjectKey, downloadObjMeta.Size, err)
		}()
		time.Sleep(1 * time.Second)
	}
	wg.Wait()
	err := <-errCh
	if err != nil {
		zlogger.Logger.Error(err)
		migrator.SetMigrationError(err)
	}

}

func (m *Migration) UploadWorker(ctx context.Context, migrator *MigrationWorker) {
	defer func() {
		migrator.CloseUploadQueue()
	}()

	downloadQueue := migrator.GetDownloadQueue()
	wg := &sync.WaitGroup{}
	for d := range downloadQueue {
		zlogger.Logger.Info("Uploading object: ", d.ObjectKey)
		migrator.PauseUpload()
		downloadObj := d
		uploadObj := &UploadObjectMeta{
			ObjectKey: downloadObj.ObjectKey,
			DoneChan:  make(chan struct{}, 1),
			ErrChan:   make(chan error, 1),
			Size:      downloadObj.Size,
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := checkDownloadStatus(downloadObj)
			if err != nil {
				zlogger.Logger.Error(err)
				migrator.SetMigrationError(err)
				return
			}
			if downloadObj.IsFileAlreadyExist {
				switch migration.skip {
				case Skip:
					migrator.UploadStart(uploadObj)
					migrator.UploadDone(uploadObj, nil)
					return
				}
			}

			defer func() {
				_ = m.fs.Remove(downloadObj.LocalPath)
			}()
			migrator.UploadStart(uploadObj)
			zlogger.Logger.Info("upload start; ", uploadObj.ObjectKey, uploadObj.Size)
			err = util.Retry(3, time.Second*5, func() error {
				err := processUpload(ctx, downloadObj)
				return err
			})

			migrator.UploadDone(uploadObj, err)
			migrator.SetMigrationError(err)
			zlogger.Logger.Info("upload done", uploadObj.ObjectKey, uploadObj.Size, err)
		}()
		time.Sleep(1 * time.Second)
	}
	wg.Wait()
}

func getRemotePath(objectKey string) string {
	return filepath.Join(migration.migrateTo, migration.bucket, objectKey)
}

func checkIsFileExist(ctx context.Context, downloadObj *DownloadObjectMeta) error {
	remotePath := getRemotePath(downloadObj.ObjectKey)

	var isFileExist bool
	err := util.Retry(3, time.Second*5, func() error {
		var err error
		isFileExist, err = migration.zStore.IsFileExist(ctx, remotePath)
		return err
	})

	if err != nil {
		zlogger.Logger.Error(err)
		return err
	}

	downloadObj.IsFileAlreadyExist = isFileExist
	return nil
}

func checkDownloadStatus(downloadObj *DownloadObjectMeta) error {
	select {
	case <-downloadObj.DoneChan:
		return nil
	case err := <-downloadObj.ErrChan:
		return err
	}
}

func processUpload(ctx context.Context, downloadObj *DownloadObjectMeta) error {
	remotePath := getRemotePath(downloadObj.ObjectKey)

	fileObj, err := migration.fs.Open(downloadObj.LocalPath)
	if err != nil {
		zlogger.Logger.Error(err)
		return err
	}

	defer fileObj.Close()

	fileInfo, err := fileObj.Stat()
	if err != nil {
		zlogger.Logger.Error(err)
		return err
	}
	mimeType, err := zboxutil.GetFileContentType(fileObj)
	if err != nil {
		zlogger.Logger.Error("content type error: ", err, " file: ", fileInfo.Name(), " objKey:", downloadObj.ObjectKey)
		return err
	}

	if downloadObj.IsFileAlreadyExist {
		switch migration.skip {
		case Replace:
			zlogger.Logger.Info("Replacing object" + downloadObj.ObjectKey + " size " + strconv.FormatInt(downloadObj.Size, 10))
			err = migration.zStore.Replace(ctx, remotePath, fileObj, fileInfo.Size(), mimeType)
		case Duplicate:
			zlogger.Logger.Info("Duplicating object" + downloadObj.ObjectKey + " size " + strconv.FormatInt(downloadObj.Size, 10))
			err = migration.zStore.Duplicate(ctx, remotePath, fileObj, fileInfo.Size(), mimeType)
		}
	} else {
		zlogger.Logger.Info("Uploading object" + downloadObj.ObjectKey + " size " + strconv.FormatInt(downloadObj.Size, 10))
		err = migration.zStore.Upload(ctx, remotePath, fileObj, fileInfo.Size(), mimeType, false)
	}

	if err != nil {
		zlogger.Logger.Error(err)
		return err
	} else {
		if migration.deleteSource {
			if err := migration.awsStore.DeleteFile(ctx, downloadObj.ObjectKey); err != nil {
				zlogger.Logger.Error(err)
			}
		}
		migration.szCtMu.Lock()
		migration.migratedSize += uint64(downloadObj.Size)
		migration.totalMigratedObjects++
		migration.szCtMu.Unlock()
		return nil
	}
}

func (m *Migration) UpdateStateFile(migrateHandler *MigrationWorker) {
	updateState, closeStateFile, err := updateKeyFunc(migration.stateFilePath)
	if err != nil {
		zlogger.Logger.Error(err)
		migrateHandler.SetMigrationError(err)
		return
	}
	defer closeStateFile()

	updateMigratedFile, closeMigratedFile, err := updateKeyFunc(filepath.Join(migration.workDir, uploadCountFileName))
	if err != nil {
		zlogger.Logger.Error(err)
		migrateHandler.SetMigrationError(err)
		return
	}
	defer closeMigratedFile()

	uploadQueue := migrateHandler.GetUploadQueue()
	var totalMigrated int
	for u := range uploadQueue {
		select {
		case <-u.DoneChan:
			updateState(u.ObjectKey)
			totalMigrated++
			updateMigratedFile(strconv.Itoa(totalMigrated))
		case <-u.ErrChan:
			return
		}
	}
}
