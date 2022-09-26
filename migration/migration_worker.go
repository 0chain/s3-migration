package migration

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	zlogger "github.com/0chain/s3migration/logger"
)

const (
	downloadConcurrencyLimit = 30
	fileSizeLimit            = int64(1024*1024) * int64(1024) * int64(5)
	uploadConcurrencyLimit   = 10
	uploadSizeLimit          = int64(1024*1024) * int64(1024) * int64(2)
	downloadSizeLimit        = int64(1024*1024) * int64(500)
)

const (
	uploadCountFileName = "uc.txt"
)

func initUploadCountFD(fPath string) (func(), func()) {
	f, err := os.Create(fPath)
	if err != nil {
		panic(err)
	}
	countMu := &sync.Mutex{}
	var count int64
	return func() {
		countMu.Lock()
		defer countMu.Unlock()
		count++
		err := f.Truncate(0)
		if err != nil {
			zlogger.Logger.Error(err)
		}
		_, err = f.Seek(0, 0)
		if err != nil {
			zlogger.Logger.Error(err)
		}
		_, err = f.WriteString(strconv.FormatInt(count, 10))
		if err != nil {
			zlogger.Logger.Error(err)
		}

	}, func() { f.Close() }
}

type MigrationWorker struct {
	diskMutex             *sync.RWMutex
	errMutex              *sync.RWMutex
	countMu               *sync.Mutex
	currentFileSizeOnDisk int64
	// ufc --> file descriptor for filecount update.
	ucf func()
	// fc --> file closing function
	fc                  func()
	downloadQueue       chan *DownloadObjectMeta
	uploadQueue         chan *UploadObjectMeta
	downloadConcurrency int32
	uploadConcurrency   int32
	errInSystem         error
	currentUploadSize   int64
	currentDownloadSize int64
}

type DownloadObjectMeta struct {
	ObjectKey          string
	Size               int64
	LocalPath          string
	DoneChan           chan struct{}
	ErrChan            chan error
	IsFileAlreadyExist bool
}

type UploadObjectMeta struct {
	ObjectKey string
	Size      int64
	DoneChan  chan struct{}
	ErrChan   chan error
}

func NewMigrationWorker(wd string) *MigrationWorker {
	mw := &MigrationWorker{
		diskMutex:     &sync.RWMutex{},
		errMutex:      &sync.RWMutex{},
		countMu:       &sync.Mutex{},
		downloadQueue: make(chan *DownloadObjectMeta, 10000),
		uploadQueue:   make(chan *UploadObjectMeta, 10000),
	}

	fPath := filepath.Join(wd, uploadCountFileName)
	mw.ucf, mw.fc = initUploadCountFD(fPath)

	return mw
}

func (m *MigrationWorker) updateFileSizeOnDisk(size int64) {
	m.diskMutex.Lock()
	m.currentFileSizeOnDisk += size
	m.diskMutex.Unlock()
}

func (m *MigrationWorker) GetDownloadQueue() <-chan *DownloadObjectMeta {
	return m.downloadQueue
}

func (m *MigrationWorker) GetUploadQueue() <-chan *UploadObjectMeta {
	return m.uploadQueue
}

func (m *MigrationWorker) incrUploadConcurrency() {
	atomic.AddInt32(&m.uploadConcurrency, 1)
}

func (m *MigrationWorker) decrUploadConcurrency() {
	atomic.AddInt32(&m.uploadConcurrency, -1)
}

func (m *MigrationWorker) checkUploadStatus() bool {
	return atomic.LoadInt32(&m.uploadConcurrency) >= uploadConcurrencyLimit || atomic.LoadInt64(&m.currentUploadSize) >= uploadSizeLimit
}

func (m *MigrationWorker) PauseUpload() {
	for m.checkUploadStatus() {
		time.Sleep(5 * time.Second)
	}
}

func (m *MigrationWorker) UploadStart(u *UploadObjectMeta) {
	m.incrUploadConcurrency()
	atomic.AddInt64(&m.currentUploadSize, u.Size)
	m.uploadQueue <- u
}

func (m *MigrationWorker) UploadDone(u *UploadObjectMeta, err error) {
	m.updateFileSizeOnDisk(-u.Size)
	m.decrUploadConcurrency()
	atomic.AddInt64(&m.currentUploadSize, -u.Size)
	if err != nil {
		u.ErrChan <- err
	} else {
		m.ucf()
	}
	u.DoneChan <- struct{}{}
}

func (m *MigrationWorker) CloseUploadQueue() {
	close(m.uploadQueue)
}

func (m *MigrationWorker) incrDownloadConcurrency() {
	atomic.AddInt32(&m.downloadConcurrency, 1)
}

func (m *MigrationWorker) decrDownloadConcurrency() {
	atomic.AddInt32(&m.downloadConcurrency, -1)
}

func (m *MigrationWorker) checkDownloadStatus() bool {
	m.diskMutex.RLock()
	defer m.diskMutex.RUnlock()
	return m.currentFileSizeOnDisk >= fileSizeLimit ||
		atomic.LoadInt32(&m.downloadConcurrency) >= downloadConcurrencyLimit ||
		atomic.LoadInt64(&m.currentDownloadSize) >= downloadSizeLimit
}

func (m *MigrationWorker) PauseDownload() {
	for m.checkDownloadStatus() {
		time.Sleep(5 * time.Second)
	}
}

func (m *MigrationWorker) DownloadStart(d *DownloadObjectMeta) {
	zlogger.Logger.Info("Started to download ", d.ObjectKey)
	m.incrDownloadConcurrency()
	m.downloadQueue <- d
	m.updateFileSizeOnDisk(d.Size)
	atomic.AddInt64(&m.currentDownloadSize, d.Size)
}

func (m *MigrationWorker) DownloadDone(d *DownloadObjectMeta, localPath string, err error) {
	m.decrDownloadConcurrency()
	atomic.AddInt64(&m.currentDownloadSize, -d.Size)
	if err != nil {
		d.ErrChan <- err
		zlogger.Logger.Error("Error while downloading ", d.ObjectKey, " Error: ", err)
	} else {
		d.LocalPath = localPath
		d.DoneChan <- struct{}{}
		zlogger.Logger.Info("Downloaded ", d.ObjectKey)
	}
}

func (m *MigrationWorker) CloseDownloadQueue() {
	close(m.downloadQueue)
}

func (m *MigrationWorker) GetMigrationError() error {
	m.errMutex.RLock()
	defer m.errMutex.RUnlock()
	return m.errInSystem
}

func (m *MigrationWorker) IsMigrationError() bool {
	return m.GetMigrationError() != nil
}

func (m *MigrationWorker) SetMigrationError(err error) {
	if err != nil {
		zlogger.Logger.Error("Setting migration error: ", err)
		m.errMutex.Lock()
		defer m.errMutex.Unlock()
		m.errInSystem = err
	}
}
