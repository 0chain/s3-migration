package util

import (
	"sync"
	"sync/atomic"
	"time"
)

const (
	downloadConcurrencyLimit = 30
	fileSizeLimit            = int64(1024*1024) * int64(1024) * int64(5)
	uploadConcurrencyLimit   = 10
	uploadSizeLimit          = int64(1024*1024) * int64(500)
	downloadSizeLimit        = int64(1024*1024) * int64(500)
)

type MigrationQueue struct {
	diskMutex             *sync.RWMutex
	errMutex              *sync.RWMutex
	currentFileSizeOnDisk int64
	downloadQueue         chan *DownloadObjectMeta
	uploadQueue           chan *UploadObjectMeta
	downloadConcurrency   int32
	uploadConcurrency     int32
	errInSystem           error
	currentUploadSize     int64
	currentDownloadSize   int64
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

func NewMigrationQueue() *MigrationQueue {
	return &MigrationQueue{
		diskMutex:     &sync.RWMutex{},
		errMutex:      &sync.RWMutex{},
		downloadQueue: make(chan *DownloadObjectMeta, 10000),
		uploadQueue:   make(chan *UploadObjectMeta, 10000),
	}
}

func (m *MigrationQueue) updateFileSizeOnDisk(size int64) {
	m.diskMutex.Lock()
	m.currentFileSizeOnDisk += size
	m.diskMutex.Unlock()
}

func (m *MigrationQueue) GetDownloadQueue() <-chan *DownloadObjectMeta {
	return m.downloadQueue
}

func (m *MigrationQueue) GetUploadQueue() <-chan *UploadObjectMeta {
	return m.uploadQueue
}

func (m *MigrationQueue) incrUploadConcurrency() {
	atomic.AddInt32(&m.uploadConcurrency, 1)
}

func (m *MigrationQueue) decrUploadConcurrency() {
	atomic.AddInt32(&m.uploadConcurrency, -1)
}

func (m *MigrationQueue) checkUploadStatus() bool {
	return atomic.LoadInt32(&m.uploadConcurrency) >= uploadConcurrencyLimit || atomic.LoadInt64(&m.currentUploadSize) >= uploadSizeLimit
}

func (m *MigrationQueue) PauseUpload() {
	for m.checkUploadStatus() {
		time.Sleep(5 * time.Second)
	}
}

func (m *MigrationQueue) UploadStart(u *UploadObjectMeta) {
	m.incrUploadConcurrency()
	atomic.AddInt64(&m.currentUploadSize, u.Size)
	m.uploadQueue <- u
}

func (m *MigrationQueue) UploadDone(u *UploadObjectMeta, err error) {
	m.updateFileSizeOnDisk(-u.Size)
	m.decrUploadConcurrency()
	atomic.AddInt64(&m.currentUploadSize, -u.Size)
	if err != nil {
		u.ErrChan <- err
	}
	u.DoneChan <- struct{}{}
}

func (m *MigrationQueue) CloseUploadQueue() {
	close(m.uploadQueue)
}

func (m *MigrationQueue) incrDownloadConcurrency() {
	atomic.AddInt32(&m.downloadConcurrency, 1)
}

func (m *MigrationQueue) decrDownloadConcurrency() {
	atomic.AddInt32(&m.downloadConcurrency, -1)
}

func (m *MigrationQueue) checkDownloadStatus() bool {
	m.diskMutex.RLock()
	defer m.diskMutex.RUnlock()
	return m.currentFileSizeOnDisk >= fileSizeLimit ||
		atomic.LoadInt32(&m.downloadConcurrency) >= downloadConcurrencyLimit ||
		atomic.LoadInt64(&m.currentDownloadSize) >= downloadSizeLimit
}

func (m *MigrationQueue) PauseDownload() {
	for m.checkDownloadStatus() {
		time.Sleep(5 * time.Second)
	}
}

func (m *MigrationQueue) DownloadStart(d *DownloadObjectMeta) {
	m.incrDownloadConcurrency()
	m.downloadQueue <- d
	m.updateFileSizeOnDisk(d.Size)
	atomic.AddInt64(&m.currentDownloadSize, d.Size)
}

func (m *MigrationQueue) DownloadDone(d *DownloadObjectMeta, localPath string, err error) {
	m.decrDownloadConcurrency()
	atomic.AddInt64(&m.currentDownloadSize, -d.Size)
	if err != nil {
		d.ErrChan <- err
	} else {
		d.LocalPath = localPath
		d.DoneChan <- struct{}{}
	}
}

func (m *MigrationQueue) CloseDownloadQueue() {
	close(m.downloadQueue)
}

func (m *MigrationQueue) GetMigrationError() error {
	m.errMutex.RLock()
	defer m.errMutex.RUnlock()
	return m.errInSystem
}

func (m *MigrationQueue) IsMigrationError() bool {
	return m.GetMigrationError() != nil
}

func (m *MigrationQueue) SetMigrationError(err error) {
	if err != nil {
		m.errMutex.Lock()
		defer m.errMutex.Unlock()
		m.errInSystem = err
	}
}
