package migration

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/0chain/s3migration/azure"
	"github.com/0chain/s3migration/box"
	"github.com/0chain/s3migration/dropbox"
	"github.com/0chain/s3migration/gdrive"
	gcloud "github.com/0chain/s3migration/google_cloud"
	"github.com/0chain/s3migration/onedrive"
	"github.com/0chain/s3migration/types"
	T "github.com/0chain/s3migration/types"
	"golang.org/x/oauth2"

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
	uploadCountFileName    = "upload.count"
	sourceDeleteFailed     = "source_delete.failed"
	downloadFailedFileName = "download.failed"
	uploadFailedFileName   = "upload.failed"
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
	key           string
	startTime     time.Time
	endTime       time.Time
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

	logsPath := filepath.Join(migration.workDir, "logs")
	if _, err := os.Stat(logsPath); err == nil {
		if err := os.RemoveAll(logsPath); err != nil {
			zlogger.Logger.Error("Failed to remove logs folder:", err)
		}
	} else if !os.IsNotExist(err) {
		zlogger.Logger.Error("Error checking logs folder:", err)
	}

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
			mConfig.NewerThan,
			mConfig.OlderThan,
		)
	} else if mConfig.Source == "box" {

		dataSourceStore, err = box.GetBoxClient(box.BoxClient{
			WorkDir:      mConfig.WorkDir,
			AccessToken:  util.GetAccessKeyFromEnv(),
			RefreshToken: util.GetRefreshKeyFromEnv(),
			NewerThan:    mConfig.NewerThan,
			OlderThan:    mConfig.OlderThan,
		})

	} else if mConfig.Source == "google_drive" || mConfig.Source == "google_cloud_storage" {
		// use client id instead of access token to prevent expiry time
		ClientID, ClientSecret := util.GetClientCredentialsFromEnv()
		var cfg oauth2.Config
		if ClientID != "" || ClientSecret != "" {
			cfg = oauth2.Config{
				ClientID:     ClientID,
				ClientSecret: ClientSecret,
				Endpoint: oauth2.Endpoint{
					AuthURL:       "https://accounts.google.com/o/oauth2/auth",
					DeviceAuthURL: "https://oauth2.googleapis.com/device/code",
					TokenURL:      "https://oauth2.googleapis.com/token",
				},
			}
		} else {
			cfg = oauth2.Config{
				Endpoint: oauth2.Endpoint{
					AuthURL:       "https://accounts.google.com/o/oauth2/auth",
					DeviceAuthURL: "https://oauth2.googleapis.com/device/code",
					TokenURL:      "https://oauth2.googleapis.com/token",
				},
			}
		}

		token := &oauth2.Token{
			AccessToken:  util.GetAccessKeyFromEnv(),
			RefreshToken: util.GetRefreshKeyFromEnv(),
		}
		if mConfig.Source == "google_drive" {
			dataSourceStore, err = gdrive.NewGoogleDriveClient(
				cfg,
				token,
				mConfig.WorkDir,
				mConfig.NewerThan,
				mConfig.OlderThan,
			)
		} else {
			dataSourceStore, err = gcloud.NewGoogleCloudClient(cfg, token, mConfig.Bucket, mConfig.NewerThan, mConfig.OlderThan)
		}
	} else if mConfig.Source == "onedrive" {
		// use access token and refresh token to prevent expiry time
		token := &oauth2.Token{
			AccessToken:  util.GetAccessKeyFromEnv(),
			RefreshToken: util.GetRefreshKeyFromEnv(),
		}
		dataSourceStore, err = onedrive.NewOneDriveClient(
			token,
			mConfig.WorkDir,
			mConfig.NewerThan,
			mConfig.OlderThan,
		)

	} else if mConfig.Source == "azure" {
		connectionString, accountName, containerName := util.GetAzureCredentials()
		dataSourceStore, err = azure.NewAzureClient(
			mConfig.WorkDir,
			accountName,
			connectionString,
			containerName,
			mConfig.NewerThan,
			mConfig.OlderThan,
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
	key := "objectKey"
	if mConfig.Source == "google_drive" || mConfig.Source == "box" {
		key = "objectName"
	}
	if mConfig.Source == "onedrive" {
		key = "Id"
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
		key:             key,
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
	migration.startTime = time.Now()

	migrationTimeFilePath := filepath.Join(migration.workDir, "migration_time.txt")
	if _, err := os.Stat(migrationTimeFilePath); err == nil {
		if err := os.Remove(migrationTimeFilePath); err != nil {
			zlogger.Logger.Error("Failed to remove migration_time.txt file: ", err)
		}
	}
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
	zlogger.Logger.Info("Total migrated objects :: ", migration.totalMigratedObjects)
	zlogger.Logger.Info("Total migrated size: ", migration.migratedSize)
	return err
}

func getValueBasedOnKey(field_name string, key string, obj types.ObjectMeta) string {
	if field_name == "objectName" {
		if key == "objectKey" || key == "Id" {
			return obj.Key
		} else if key == "objectName" {
			if obj.Name != nil {
				return *obj.Name
			}
		}
	} else if field_name == "objectKey" {
		if key == "Id" {
			return *obj.Id
		} else {
			return obj.Key
		}
	}
	return ""
}

func (m *Migration) DownloadWorker(ctx context.Context, migrator *MigrationWorker) {
	defer migrator.CloseDownloadQueue()

	downloadStartTime := time.Now()

	var allObjects []*T.ObjectMeta
	var totalCount int
	var totalSize int64
	nameMap := make(map[string]bool) // Track existing names

	downloadFailedFile, err2 := os.OpenFile(filepath.Join(m.workDir, downloadFailedFileName), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err2 != nil {
		zlogger.Logger.Error("Failed to create download failed file:", err2)
	}
	defer downloadFailedFile.Close()

	objCh, errCh := migration.dataSourceStore.ListFiles(rootContext)

	for obj := range objCh {
		baseName := getValueBasedOnKey("objectName", migration.key, *obj)
		if _, exists := nameMap[baseName]; exists {
			h := sha1.New()
			uniqueInput := fmt.Sprintf("%s-%s-%d", obj.Key, baseName, time.Now().UnixNano())
			h.Write([]byte(uniqueInput))
			uniqueSuffix := hex.EncodeToString(h.Sum(nil))[:8] // Use first 8 chars of hash

			newName := fmt.Sprintf("%s_%s", baseName, uniqueSuffix)
			obj.Name = &newName
		} else {
			nameMap[baseName] = true
		}

		allObjects = append(allObjects, obj)
		totalCount++
		totalSize += obj.Size
		zlogger.Logger.Info(fmt.Sprintf("Discovered object: %s, size: %d", obj.Key, obj.Size))
	}

	go func() {
		f, err := os.Create(filepath.Join(m.workDir, "files.count"))
		if err != nil {
			zlogger.Logger.Error(err)
			return
		}
		defer f.Close()
		_, err = f.WriteString(strconv.Itoa(totalCount))
		if err != nil {
			zlogger.Logger.Error(err)
		}
		zlogger.Logger.Info(fmt.Sprintf("Total files to migrate: %d", totalCount))
	}()

	if err := <-errCh; err != nil {
		zlogger.Logger.Error("Error listing files:", err)
		migrator.SetMigrationError(err)
		return
	}

	listingTime := time.Since(downloadStartTime)
	estimatedDownloadTime := listingTime * time.Duration(totalCount)
	estimatedTotalTime := estimatedDownloadTime * 20

	go func() {
		migrationTimeFilePath := filepath.Join(m.workDir, "migration_time.txt")
		estimateStr := fmt.Sprintf("Estimated time: %v\nFiles to process: %d\nTotal size: %d bytes",
			estimatedTotalTime, totalCount, totalSize)
		if err := os.WriteFile(migrationTimeFilePath, []byte(estimateStr), 0644); err != nil {
			zlogger.Logger.Error("Failed to write estimated migration time:", err)
		}
	}()

	wg := &sync.WaitGroup{}
	ops := make([]MigrationOperation, 0, m.batchSize)
	var opLock sync.Mutex
	currentSize := 0
	opCtx, opCtxCancel := context.WithCancel(ctx)

	for _, obj := range allObjects {
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
		zlogger.Logger.Info("Downloading object info ", obj.Key, obj.Name, obj.Size)
		downloadObjMeta := &DownloadObjectMeta{
			ObjectKey:  getValueBasedOnKey("objectKey", migration.key, *obj),
			ObjectName: getValueBasedOnKey("objectName", migration.key, *obj),
			Size:       obj.Size,
			DoneChan:   make(chan struct{}, 1),
			ErrChan:    make(chan error, 1),
			mimeType:   obj.ContentType,
			ParentPath: *&obj.ParentPath,
		}

		wg.Add(1)
		go func(objMeta *DownloadObjectMeta) {
			defer func(start time.Time) {
				zlogger.Logger.Info("downloadObjMeta key:  ", objMeta.ObjectName, time.Since(start))
				if err := m.logFileStatus(objMeta.ObjectName, objMeta.Size, "DOWNLOAD_COMPLETED", ""); err != nil {
					zlogger.Logger.Error("Failed to log file status: ", err)
				}
			}(time.Now())

			defer wg.Done()
			err := checkIsFileExist(ctx, objMeta)
			if err != nil {
				zlogger.Logger.Error("check file error: ", err)
				// Log failed download but continue
				if _, writeErr := downloadFailedFile.WriteString(fmt.Sprintf("%s\t%s\n", objMeta.ObjectName, err.Error())); writeErr != nil {
					zlogger.Logger.Error("Failed to write to download failed file:", writeErr)
				}
				migrator.SetMigrationError(err)
				return
			}

			dataChan := make(chan *util.DataChan, 200)
			streamWriter := util.NewStreamWriter(dataChan)
			go m.processChunkDownload(opCtx, streamWriter, migrator, objMeta)

			op, _ := processOperationForMemory(ctx, objMeta, streamWriter)
			opLock.Lock()
			ops = append(ops, op)
			opLock.Unlock()
		}(downloadObjMeta)
	}

	// Process any remaining operations
	if currentSize > 0 {
		wg.Wait()
		processOps := ops
		m.processMultiOperation(ctx, processOps, migrator)
		ops = nil
	}

	downloadTime := time.Since(downloadStartTime)
	estimatedRemainingTime := downloadTime * 2
	totalEstimatedTime := downloadTime + estimatedRemainingTime

	go func() {
		migrationTimeFilePath := filepath.Join(m.workDir, "migration_time_actual.txt")
		timeStr := fmt.Sprintf("Download time: %v\nEstimated total time: %v\nFiles processed: %d\nTotal size: %d bytes",
			downloadTime, totalEstimatedTime, totalCount, totalSize)
		if err := os.WriteFile(migrationTimeFilePath, []byte(timeStr), 0644); err != nil {
			zlogger.Logger.Error("Failed to write actual migration time:", err)
		}
	}()

	opCtxCancel()
	wg.Wait()

	migrator.CloseUploadQueue()
}

func (m *Migration) UploadWorker(ctx context.Context, migrator *MigrationWorker) {
	defer func() {
		migrator.CloseUploadQueue()
	}()

	// Create failed uploads file
	uploadFailedFile, err := os.OpenFile(filepath.Join(m.workDir, uploadFailedFileName), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		zlogger.Logger.Error("Failed to create upload failed file:", err)
	}
	defer uploadFailedFile.Close()

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
			// Log failed upload but continue
			if _, writeErr := uploadFailedFile.WriteString(fmt.Sprintf("%s\t%s\n", d.ObjectName, err.Error())); writeErr != nil {
				zlogger.Logger.Error("Failed to write to upload failed file:", writeErr)
			}
			if err := m.logFileStatus(d.ObjectName, d.Size, "UPLOAD_FAILED", err.Error()); err != nil {
				zlogger.Logger.Error("Failed to log file status: ", err)
			}
			continue
		}
		if downloadObj.IsFileAlreadyExist {
			switch migration.skip {
			case Skip:
				migrator.UploadStart(uploadObj)
				migrator.UploadDone(uploadObj, nil)
				if err := m.logFileStatus(d.ObjectName, d.Size, "UPLOAD_SKIPPED", "File already exists"); err != nil {
					zlogger.Logger.Error("Failed to log file status: ", err)
				}
				continue
			}
		}
		op, err := processOperation(ctx, downloadObj)
		if err != nil {
			zlogger.Logger.Error(err)
			// Log failed upload but continue
			if _, writeErr := uploadFailedFile.WriteString(fmt.Sprintf("%s\t%s\n", d.ObjectName, err.Error())); writeErr != nil {
				zlogger.Logger.Error("Failed to write to upload failed file:", writeErr)
			}
			if err := m.logFileStatus(d.ObjectName, d.Size, "UPLOAD_FAILED", err.Error()); err != nil {
				zlogger.Logger.Error("Failed to log file status: ", err)
			}
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
		if err := m.logFileStatus(d.ObjectName, d.Size, "UPLOAD_COMPLETED", ""); err != nil {
			zlogger.Logger.Error("Failed to log file status: ", err)
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

func getRemotePath(objectKey string, parentPath interface{}) string {
	var fullPath string

	if parentPathStr, ok := parentPath.(*string); ok && parentPathStr != nil && *parentPathStr != "" {
		fullPath = path.Join(migration.migrateTo, migration.bucket, *parentPathStr, getUniqueShortObjKey(objectKey))
	} else {
		fullPath = path.Join(migration.migrateTo, migration.bucket, getUniqueShortObjKey(objectKey))
	}

	return fullPath
}

func checkIsFileExist(ctx context.Context, downloadObj *DownloadObjectMeta) error {
	remotePath := getRemotePath(downloadObj.ObjectName, downloadObj.ParentPath)

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
		zlogger.Logger.Info("uploading object key:  ", downloadObj.ObjectName, time.Since(start))
	}(time.Now())

	remotePath := getRemotePath(downloadObj.ObjectName, downloadObj.ParentPath)
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
		zlogger.Logger.Error("content type error: ", err, " file: ", fileInfo.Name(), " objKey:", downloadObj.ObjectName)
		return op, err
	}
	var fileOperation sdk.OperationRequest
	if downloadObj.IsFileAlreadyExist {
		switch migration.skip {
		case Replace:
			zlogger.Logger.Info("Replacing object" + downloadObj.ObjectName + " size " + strconv.FormatInt(downloadObj.Size, 10))
			fileOperation = migration.zStore.Replace(ctx, remotePath, fileObj, downloadObj.Size, mimeType)
		case Duplicate:
			zlogger.Logger.Info("Duplicating object" + downloadObj.ObjectName + " size " + strconv.FormatInt(downloadObj.Size, 10))
			fileOperation = migration.zStore.Duplicate(ctx, remotePath, fileObj, downloadObj.Size, mimeType)
		}
	} else {
		zlogger.Logger.Info("Uploading object: " + downloadObj.ObjectName + " size " + strconv.FormatInt(downloadObj.Size, 10))
		fileOperation = migration.zStore.Upload(ctx, remotePath, fileObj, downloadObj.Size, mimeType, false)
	}
	op.Operation = fileOperation
	return op, nil
}

func processOperationForMemory(ctx context.Context, downloadObj *DownloadObjectMeta, r io.Reader) (MigrationOperation, error) {
	remotePath := getRemotePath(downloadObj.ObjectName, downloadObj.ParentPath)
	var op MigrationOperation
	mimeType := downloadObj.mimeType
	var fileOperation sdk.OperationRequest
	if downloadObj.IsFileAlreadyExist {
		switch migration.skip {
		case Replace:
			zlogger.Logger.Info("Replacing object" + downloadObj.ObjectName + " size " + strconv.FormatInt(downloadObj.Size, 10))
			fileOperation = migration.zStore.Replace(ctx, remotePath, r, downloadObj.Size, mimeType)
		case Duplicate:
			zlogger.Logger.Info("Duplicating object " + downloadObj.ObjectName + " size " + strconv.FormatInt(downloadObj.Size, 10))
			fileOperation = migration.zStore.Duplicate(ctx, remotePath, r, downloadObj.Size, mimeType)
		}
	} else {
		zlogger.Logger.Info("Uploading object: " + downloadObj.ObjectName + " size " + strconv.FormatInt(downloadObj.Size, 10))
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

	// write for file to be uploaded

	updateMigratedFile, closeMigratedFile, err := updateKeyFunc(filepath.Join(m.workDir, uploadCountFileName))
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
			if totalMigrated == 0 {
				currentTime := time.Now()
				elapsedTime := currentTime.Sub(migration.startTime)

				err := os.WriteFile(filepath.Join(m.workDir, "migration_time.txt"), []byte(fmt.Sprintf("%v", elapsedTime)), 0644)
				if err != nil {
					zlogger.Logger.Error(err)
				}
			}
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
		zlogger.Logger.Info("upload done for object key: ", op.uploadObj.ObjectKey, " size ", op.uploadObj.Size, err)
	}
	migrator.SetMigrationError(err)
	return err
}

func (m *Migration) processChunkDownload(ctx context.Context, sw *util.StreamWriter, migrator *MigrationWorker, downloadObjMeta *DownloadObjectMeta) {
	// chunk download and pipe data

	migrator.DownloadStart(downloadObjMeta)
	zlogger.Logger.Info("Downloading object: ", downloadObjMeta.ObjectName)
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

func (m *Migration) logFileStatus(objectName string, size int64, status string, message string) error {
	logEntry := struct {
		ObjectName string    `json:"object_name"`
		Size       int64     `json:"size"`
		Status     string    `json:"status"`
		Message    string    `json:"message,omitempty"`
		Time       time.Time `json:"timestamp"`
	}{
		ObjectName: objectName,
		Size:       size,
		Status:     status,
		Message:    message,
		Time:       time.Now(),
	}

	logFile := filepath.Join(m.workDir, "migration_status.log")
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %v", err)
	}
	defer f.Close()

	logJSON, err := json.Marshal(logEntry)
	if err != nil {
		return fmt.Errorf("failed to marshal log entry: %v", err)
	}

	if _, err := f.WriteString(string(logJSON) + "\n"); err != nil {
		return fmt.Errorf("failed to write log entry: %v", err)
	}

	return nil
}
