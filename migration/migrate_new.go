package migration

import (
	"context"
	"github.com/0chain/gosdk/zboxcore/zboxutil"
	zlogger "github.com/0chain/s3migration/logger"
	"github.com/0chain/s3migration/util"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

func (m *Migration) DownloadWorker(ctx context.Context, migrator *util.MigrationQueue) {
	defer migrator.CloseDownloadQueue()
	objCh, errCh := migration.awsStore.ListFilesInBucket(rootContext)
	wg := &sync.WaitGroup{}
	for obj := range objCh {
		migrator.PauseDownload()
		if migrator.IsMigrationError() {
			return
		}
		wg.Add(1)

		downloadObjMeta := &util.DownloadObjectMeta{
			ObjectKey: obj.Key,
			Size:      obj.Size,
			DoneChan:  make(chan struct{}, 1),
			ErrChan:   make(chan error, 1),
		}

		go func() {
			defer wg.Done()
			err := checkIsFileExist(ctx, downloadObjMeta)
			if err != nil {
				migrator.SetMigrationError(err)
				return
			}
			if downloadObjMeta.IsFileAlreadyExist && migration.skip == Skip {
				zlogger.Logger.Info("Skipping migration of object" + downloadObjMeta.ObjectKey)
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
	select {
	case err := <-errCh:
		if err != nil {
			migrator.SetMigrationError(err)
		}
	}
}

func (m *Migration) UploadWorker(ctx context.Context, migrator *util.MigrationQueue) {
	defer migrator.CloseUploadQueue()
	downloadQueue := migrator.GetDownloadQueue()
	wg := &sync.WaitGroup{}
	for d := range downloadQueue {
		migrator.PauseUpload()
		downloadObj := d
		uploadObj := &util.UploadObjectMeta{
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
				migrator.SetMigrationError(err)
				return
			}
			defer os.Remove(downloadObj.LocalPath)
			migrator.UploadStart(uploadObj)
			zlogger.Logger.Info("upload start", uploadObj.ObjectKey, uploadObj.Size)
			err = util.Retry(3, time.Second*5, func() error {
				var err error
				err = processUpload(ctx, downloadObj)
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

func checkIsFileExist(ctx context.Context, downloadObj *util.DownloadObjectMeta) error {
	remotePath := getRemotePath(downloadObj.ObjectKey)

	var isFileExist bool
	var err error
	err = util.Retry(3, time.Second*5, func() error {
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

func checkDownloadStatus(downloadObj *util.DownloadObjectMeta) error {
	select {
	case <-downloadObj.DoneChan:
		return nil
	case err := <-downloadObj.ErrChan:
		return err
	}
}

func processUpload(ctx context.Context, downloadObj *util.DownloadObjectMeta) error {
	remotePath := getRemotePath(downloadObj.ObjectKey)

	fileObj, err := os.Open(downloadObj.LocalPath)
	if err != nil {
		zlogger.Logger.Error(err)
		return err
	}

	defer fileObj.Close()

	fileInfo, err := fileObj.Stat()
	mimeType, err := zboxutil.GetFileContentType(fileObj)
	if err != nil {
		zlogger.Logger.Error(err)
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
			migration.awsStore.DeleteFile(ctx, downloadObj.ObjectKey)
		}
		migration.szCtMu.Lock()
		migration.migratedSize += uint64(downloadObj.Size)
		migration.totalMigratedObjects++
		migration.szCtMu.Unlock()
		return nil
	}
}

func (m *Migration) UpdateStateFile(migrateHandler *util.MigrationQueue) {
	updateState, closeStateFile, err := updateStateKeyFunc(migration.stateFilePath)
	if err != nil {
		migrateHandler.SetMigrationError(err)
		return
	}
	defer closeStateFile()
	uploadQueue := migrateHandler.GetUploadQueue()
	for u := range uploadQueue {
		select {
		case <-u.DoneChan:
			updateState(u.ObjectKey)
		case <-u.ErrChan:
			return
		}
	}
	return
}

func MigrationStart() error {
	defer func(start time.Time) {
		zlogger.Logger.Info("time taken: ", time.Now().Sub(start))
	}(time.Now())

	migrateHandler := util.NewMigrationQueue()
	go migration.DownloadWorker(rootContext, migrateHandler)
	go migration.UploadWorker(rootContext, migrateHandler)
	migration.UpdateStateFile(migrateHandler)
	err := migrateHandler.GetMigrationError()
	if err != nil {
		zlogger.Logger.Error("Error while migration, err", err)
	}
	zlogger.Logger.Info("Total migrated objects: ", migration.totalMigratedObjects)
	zlogger.Logger.Info("Total migrated size: ", migration.migratedSize)
	return err
}
