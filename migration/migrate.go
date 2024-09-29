package migration

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/0chain/s3migration/dropbox"
	"github.com/0chain/s3migration/gdrive"
	T "github.com/0chain/s3migration/types"

	"github.com/0chain/gosdk/zboxcore/sdk"
	"github.com/0chain/gosdk/zboxcore/zboxutil"
	dStorage "github.com/0chain/s3migration/dstorage"
	zlogger "github.com/0chain/s3migration/logger"
	"github.com/0chain/s3migration/s3"
	"github.com/0chain/s3migration/util"
	zerror "github.com/0chain/s3migration/zErrors"
)

const Batch = 10
const (
	Replace   = iota // Will replace existing file
	Skip             // Will skip migration if file already exists
	Duplicate        // Will add _copy prefix and uploads the file
)

const (
	uploadCountFileName = "upload.count"
	sourceDeleteFailed  = "source_delete.failed"
)

const (
	maxBatchSize = 1024 * 1024 * 1024 // 1GB
)

var migration Migration

// Use context for all requests.
var rootContext context.Context
var rootContextCancel context.CancelFunc
var dsFileHandler io.WriteCloser

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
	zStore          dStorage.DStoreI
	dataSourceStore T.CloudStorageI
	fs              util.FileSystem

	skip       int
	retryCount int

	// Number of goroutines to run. So at most concurrency * Batch goroutines will run. i.e. for bucket level and object level
	concurrency int

	szCtMu               sync.Mutex // size and count mutex; used to update migratedSize and totalMigratedObjects
	migratedSize         uint64
	totalMigratedObjects uint64

	stateFilePath string
	migrateTo     string
	workDir       string
	deleteSource  bool
	bucket        string
	chunkSize     int64
	batchSize     int
}

type MigrationOperation struct {
	Operation sdk.OperationRequest
	uploadObj *UploadObjectMeta
}

func updateTotalObjects(totalObjChan chan struct{}, wd string) error {
	f, err := os.Create(filepath.Join(wd, "files.total"))
	if err != nil {
		return err
	}
	defer f.Close()
	var totalFiles int

	for range totalObjChan {
		totalFiles++
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
		mConfig.ChunkNumber,
		mConfig.BatchSize,
	)
	if err != nil {
		zlogger.Logger.Error(err)
		return err
	}
	mConfig.ChunkSize = int64(mConfig.ChunkNumber) * dStorageService.GetChunkWriteSize()
	zlogger.Logger.Info(fmt.Sprintf("Getting %v storage service", mConfig.Source))

	var dataSourceStore T.CloudStorageI
	if mConfig.Source == "s3" {
		dataSourceStore, err = s3.GetAwsClient(
			mConfig.Bucket,
			mConfig.Prefix,
			mConfig.Region,
			mConfig.DeleteSource,
			mConfig.NewerThan,
			mConfig.OlderThan,
			mConfig.StartAfter,
			mConfig.WorkDir,
		)
	} else if mConfig.Source == "dropbox" {
		dataSourceStore, err = dropbox.GetDropboxClient(
			util.GetAccessKeyFromEnv(),
			mConfig.WorkDir,
		)
	} else if mConfig.Source == "google_drive" {
		dataSourceStore, err = gdrive.NewGoogleDriveClient(
			util.GetAccessKeyFromEnv(),
			mConfig.WorkDir,
		)
	} else {
		zlogger.Logger.Error("invalid source: ", mConfig.Source)
		return err
	}

	zlogger.Logger.Info(dataSourceStore, "data source info")
	if err != nil {
		zlogger.Logger.Error(err)
		return err
	}

	migration = Migration{
		zStore:          dStorageService,
		dataSourceStore: dataSourceStore,
		skip:            mConfig.Skip,
		concurrency:     mConfig.Concurrency,
		retryCount:      mConfig.RetryCount,
		stateFilePath:   mConfig.StateFilePath,
		migrateTo:       mConfig.MigrateToPath,
		deleteSource:    mConfig.DeleteSource,
		workDir:         mConfig.WorkDir,
		bucket:          mConfig.Bucket,
		fs:              util.Fs,
		chunkSize:       mConfig.ChunkSize,
		batchSize:       mConfig.BatchSize,
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

	if migration.deleteSource {
		f, err := os.Create(filepath.Join(migration.workDir, sourceDeleteFailed))
		if err != nil {
			return err
		}
		dsFileHandler = f
		defer dsFileHandler.Close()
	}

	migrationWorker := NewMigrationWorker(migration.workDir)

	wg := sync.WaitGroup{}
	wg.Add(2)
	go func() {
		defer wg.Done()
		migration.DownloadWorker(rootContext, migrationWorker)
	}()
	// go migration.UploadWorker(rootContext, migrationWorker)
	go func() {
		migration.UpdateStateFile(migrationWorker)
		wg.Done()
	}()
	wg.Wait()
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
	totalObjChan := make(chan struct{}, 100)
	defer close(totalObjChan)
	go updateTotalObjects(totalObjChan, m.workDir)
	objCh, errCh := migration.dataSourceStore.ListFiles(rootContext)
	wg := &sync.WaitGroup{}
	ops := make([]MigrationOperation, 0, m.batchSize)
	var opLock sync.Mutex
	currentSize := 0
	opCtx, opCtxCancel := context.WithCancel(ctx)
	for obj := range objCh {
		zlogger.Logger.Info("Downloading object: ", obj.Key)
		migrator.PauseDownload()
		if migrator.IsMigrationError() {
			opCtxCancel()
			return
		}
		if currentSize >= m.batchSize {
			// Here scope of improvement
			wg.Wait()
			if len(ops) > 0 {
				m.processMultiOperation(opCtx, ops, migrator)
			} else {
				zlogger.Logger.Info("No operation to process")
			}
			opCtxCancel()
			opCtx, opCtxCancel = context.WithCancel(ctx)
			ops = nil
			currentSize = 0
		}
		currentSize++
		downloadObjMeta := &DownloadObjectMeta{
			ObjectKey: obj.Key,
			Size:      obj.Size,
			DoneChan:  make(chan struct{}, 1),
			ErrChan:   make(chan error, 1),
			mimeType:  obj.ContentType,
		}
		wg.Add(1)
		go func() {
			defer func(start time.Time) {
				zlogger.Logger.Info("downloadObjMeta key:  ", downloadObjMeta.ObjectKey, time.Since(start))
			}(time.Now())

			defer wg.Done()
			err := checkIsFileExist(ctx, downloadObjMeta)
			if err != nil {
				zlogger.Logger.Error("check file error: ", err)
				migrator.SetMigrationError(err)
				return
			}
			if downloadObjMeta.IsFileAlreadyExist && migration.skip == Skip {
				zlogger.Logger.Info("Skipping migration of object" + downloadObjMeta.ObjectKey)
				migrator.DownloadStart(downloadObjMeta)
				migrator.DownloadDone(downloadObjMeta, "", nil)
				return
			}
			dataChan := make(chan *util.DataChan, 200)
			streamWriter := util.NewStreamWriter(dataChan)
			go m.processChunkDownload(opCtx, streamWriter, migrator, downloadObjMeta)
			// Always return nil as error
			totalObjChan <- struct{}{}
			op, _ := processOperationForMemory(ctx, downloadObjMeta, streamWriter)
			opLock.Lock()
			ops = append(ops, op)
			opLock.Unlock()
		}()
	}
	if currentSize > 0 {
		wg.Wait()
		processOps := ops
		// Here scope of improvement
		m.processMultiOperation(ctx, processOps, migrator)
		ops = nil
	}
	opCtxCancel()
	wg.Wait()
	err := <-errCh
	if err != nil {
		zlogger.Logger.Error(err)
		migrator.SetMigrationError(err)
	}
	migrator.CloseUploadQueue()
}

func (m *Migration) UploadWorker(ctx context.Context, migrator *MigrationWorker) {
	defer func() {
		migrator.CloseUploadQueue()
	}()

	downloadQueue := migrator.GetDownloadQueue()
	wg := &sync.WaitGroup{}
	ops := make([]MigrationOperation, 0, m.batchSize)
	totalSize := int64(0)
	for d := range downloadQueue {
		zlogger.Logger.Info("Uploading object: ", d.ObjectKey)
		migrator.PauseUpload()
		downloadObj := d
		uploadObj := &UploadObjectMeta{
			ObjectKey: downloadObj.ObjectKey,
			DoneChan:  make(chan struct{}, 1),
			ErrChan:   make(chan error, 1),
			Size:      downloadObj.Size,
			LocalPath: downloadObj.LocalPath,
		}
		err := checkDownloadStatus(downloadObj)
		if err != nil {
			zlogger.Logger.Error(err)
			migrator.SetMigrationError(err)
			continue
		}
		if downloadObj.IsFileAlreadyExist {
			switch migration.skip {
			case Skip:
				migrator.UploadStart(uploadObj)
				migrator.UploadDone(uploadObj, nil)
				continue
			}
		}
		op, err := processOperation(ctx, downloadObj)
		if err != nil {
			zlogger.Logger.Error(err)
			migrator.SetMigrationError(err)
			continue
		}
		op.uploadObj = uploadObj
		ops = append(ops, op)
		totalSize += downloadObj.Size
		if len(ops) >= m.batchSize || totalSize >= maxBatchSize {
			processOps := ops
			ops = nil
			wg.Add(1)
			go func(ops []MigrationOperation) {
				m.processMultiOperation(ctx, ops, migrator)
				wg.Done()
			}(processOps)
			totalSize = 0
			time.Sleep(1 * time.Second)
		}
	}
	if len(ops) > 0 {
		wg.Add(1)
		go func(ops []MigrationOperation) {
			m.processMultiOperation(ctx, ops, migrator)
			wg.Done()
		}(ops)
	}
	wg.Wait()
}

func getUniqueShortObjKey(objectKey string) string {
	// Max length to which objectKey would be trimmed to.
	// Keeping this less than 100 chars to prevent longer name in case of uploading duplicate
	// files with `_copy` suffixes.
	const maxLength = 90

	if len(objectKey) > maxLength {
		// Generate a SHA-1 hash of the object key
		hash := sha1.New()
		hash.Write([]byte(objectKey))
		hashSum := hash.Sum(nil)

		// Convert the hash to a hexadecimal string
		hashString := hex.EncodeToString(hashSum)

		// Combine the first 10 characters of the hash with a truncated object key
		shortKey := fmt.Sprintf("%s_%s", hashString[:10], objectKey[11+len(objectKey)-maxLength:])
		return shortKey
	}

	return objectKey
}

func getRemotePath(objectKey string) string {
	return filepath.Join(migration.migrateTo, migration.bucket, getUniqueShortObjKey(objectKey))
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

func processOperation(ctx context.Context, downloadObj *DownloadObjectMeta) (MigrationOperation, error) {

	defer func(start time.Time) {
		zlogger.Logger.Info("uploading object key:  ", downloadObj.ObjectKey, time.Since(start))
	}(time.Now())

	remotePath := getRemotePath(downloadObj.ObjectKey)
	var op MigrationOperation
	fileObj, err := migration.fs.Open(downloadObj.LocalPath)
	if err != nil {
		zlogger.Logger.Error(err)
		return op, err
	}
	fileInfo, err := fileObj.Stat()
	if err != nil {
		zlogger.Logger.Error(err)
		return op, err
	}
	mimeType, err := zboxutil.GetFileContentType(path.Ext(fileInfo.Name()), fileObj)
	if err != nil {
		zlogger.Logger.Error("content type error: ", err, " file: ", fileInfo.Name(), " objKey:", downloadObj.ObjectKey)
		return op, err
	}
	var fileOperation sdk.OperationRequest
	if downloadObj.IsFileAlreadyExist {
		switch migration.skip {
		case Replace:
			zlogger.Logger.Info("Replacing object" + downloadObj.ObjectKey + " size " + strconv.FormatInt(downloadObj.Size, 10))
			fileOperation = migration.zStore.Replace(ctx, remotePath, fileObj, downloadObj.Size, mimeType)
		case Duplicate:
			zlogger.Logger.Info("Duplicating object" + downloadObj.ObjectKey + " size " + strconv.FormatInt(downloadObj.Size, 10))
			fileOperation = migration.zStore.Duplicate(ctx, remotePath, fileObj, downloadObj.Size, mimeType)
		}
	} else {
		zlogger.Logger.Info("Uploading object: " + downloadObj.ObjectKey + " size " + strconv.FormatInt(downloadObj.Size, 10))
		fileOperation = migration.zStore.Upload(ctx, remotePath, fileObj, downloadObj.Size, mimeType, false)
	}
	op.Operation = fileOperation
	return op, nil
}

func processOperationForMemory(ctx context.Context, downloadObj *DownloadObjectMeta, r io.Reader) (MigrationOperation, error) {
	remotePath := getRemotePath(downloadObj.ObjectKey)
	var op MigrationOperation
	mimeType := downloadObj.mimeType
	var fileOperation sdk.OperationRequest
	if downloadObj.IsFileAlreadyExist {
		switch migration.skip {
		case Replace:
			zlogger.Logger.Info("Replacing object" + downloadObj.ObjectKey + " size " + strconv.FormatInt(downloadObj.Size, 10))
			fileOperation = migration.zStore.Replace(ctx, remotePath, r, downloadObj.Size, mimeType)
		case Duplicate:
			zlogger.Logger.Info("Duplicating object " + downloadObj.ObjectKey + " size " + strconv.FormatInt(downloadObj.Size, 10))
			fileOperation = migration.zStore.Duplicate(ctx, remotePath, r, downloadObj.Size, mimeType)
		}
	} else {
		zlogger.Logger.Info("Uploading object: " + downloadObj.ObjectKey + " size " + strconv.FormatInt(downloadObj.Size, 10))
		fileOperation = migration.zStore.Upload(ctx, remotePath, r, downloadObj.Size, mimeType, false)
	}
	op.Operation = fileOperation
	op.uploadObj = &UploadObjectMeta{
		ObjectKey: downloadObj.ObjectKey,
		DoneChan:  make(chan struct{}, 1),
		ErrChan:   make(chan error, 1),
		Size:      downloadObj.Size,
		LocalPath: downloadObj.LocalPath,
	}
	return op, nil
}

func processUpload(ctx context.Context, ops []sdk.OperationRequest) error {

	err := migration.zStore.MultiUpload(ctx, ops)
	if err != nil {
		zlogger.Logger.Error(err)
	}
	return err
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

func (m *Migration) processMultiOperation(ctx context.Context, ops []MigrationOperation, migrator *MigrationWorker) error {
	var err error
	defer func() {
		for _, op := range ops {
			if migration.deleteSource && err == nil {
				if deleteErr := migration.dataSourceStore.DeleteFile(ctx, op.uploadObj.ObjectKey); deleteErr != nil {
					zlogger.Logger.Error(deleteErr)
					dsFileHandler.Write([]byte(op.uploadObj.ObjectKey + "\n"))
				}
			}
			if err == nil {
				migration.szCtMu.Lock()
				migration.migratedSize += uint64(op.uploadObj.Size)
				migration.totalMigratedObjects++
				migration.szCtMu.Unlock()
			}
			if closer, ok := op.Operation.FileReader.(*util.FileReader); ok {
				_ = closer.Close()
			}
			_ = migration.fs.Remove(op.uploadObj.LocalPath)
		}
	}()
	fileOps := make([]sdk.OperationRequest, 0, len(ops))
	for _, op := range ops {
		migrator.UploadStart(op.uploadObj)
		zlogger.Logger.Info("upload start: ", op.uploadObj.ObjectKey, " size: ", op.uploadObj.Size)
		fileOps = append(fileOps, op.Operation)
	}
	err = util.Retry(1, time.Second*5, func() error {
		err := processUpload(ctx, fileOps)
		if err != nil {
			for _, op := range ops {
				if reader, ok := op.Operation.FileReader.(*util.FileReader); ok {
					_, _ = reader.Seek(0, io.SeekStart)
				}
			}
		}
		return err
	})
	for _, op := range ops {
		migrator.UploadDone(op.uploadObj, err)
		zlogger.Logger.Info("upload done: ", op.uploadObj.ObjectKey, " size ", op.uploadObj.Size, err)
	}
	migrator.SetMigrationError(err)
	return err
}

func (m *Migration) processChunkDownload(ctx context.Context, sw *util.StreamWriter, migrator *MigrationWorker, downloadObjMeta *DownloadObjectMeta) {
	// chunk download and pipe data

	migrator.DownloadStart(downloadObjMeta)
	offset := 0
	chunkSize := int(m.chunkSize)
	acceptedChunkSize := int(m.zStore.GetChunkWriteSize())
	defer close(sw.DataChan)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		data, err := m.dataSourceStore.DownloadToMemory(ctx, downloadObjMeta.ObjectKey, int64(offset), int64(chunkSize), downloadObjMeta.Size)
		if err != nil {
			migrator.DownloadDone(downloadObjMeta, "", err)
			ctx.Err()
			return
		}
		if len(data) > 0 {
			current := 0
			for ; current < len(data); current += acceptedChunkSize {
				high := current + acceptedChunkSize
				if high > len(data) {
					high = len(data)
				}
				sw.Write(data[current:high])
			}
		}
		offset += chunkSize
		// End of file
		if len(data) < chunkSize {
			break
		}
	}
	migrator.DownloadDone(downloadObjMeta, "", nil)
}
