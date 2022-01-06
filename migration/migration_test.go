package migration

import (
	"context"
	"errors"
	mock_dstorage "github.com/0chain/s3migration/dstorage/mocks"
	"github.com/0chain/s3migration/s3"
	mock_s3 "github.com/0chain/s3migration/s3/mocks"
	"github.com/golang/mock/gomock"
	"log"
	"testing"
)

func TestMigrate(t *testing.T) {

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	dStorageService := mock_dstorage.NewMockDStoreI(ctrl)
	awsStorageService := mock_s3.NewMockAwsI(ctrl)
	migration = Migration{
		zStore:   dStorageService,
		awsStore: awsStorageService,
		skip:     Skip,
		//concurrency:   mConfig.Concurrency,
		//retryCount:    mConfig.RetryCount,
		//stateFilePath: mConfig.StateFilePath,
		//migrateTo:     mConfig.MigrateToPath,
		//deleteSource:  mConfig.DeleteSource,
		//workDir:       mConfig.WorkDir,
	}

	isMigrationInitialized = true
	tests := []struct {
		name             string
		setUpMock        func()
		wantErr          bool
		err              error
		migrateFileCount int
	}{
		{
			name: "insufficient allocation space",
			setUpMock: func() {
				rootContext, rootContextCancel = context.WithCancel(context.Background())
				fileListChan := make(chan *s3.ObjectMeta, 1000)
				fileListChan <- &s3.ObjectMeta{
					Key: "file1", Size: 1200,
				}

				fileListChan <- &s3.ObjectMeta{
					Key: "file2", Size: 1400,
				}

				fileListChan <- &s3.ObjectMeta{
					Key: "file3", Size: 1500,
				}

				close(fileListChan)

				errChan := make(chan error, 1)
				awsStorageService.EXPECT().ListFilesInBucket(gomock.Any()).Return(fileListChan, errChan)

				updateStateKeyFunc = func(statePath string) (func(stateKey string), func(), error) {
					return func(stateKey string) {}, func() {}, nil
				}

				dStorageService.EXPECT().UpdateAllocationDetails().Return(nil)
				dStorageService.EXPECT().GetAvailableSpace().Return(int64(1200))
			},
			wantErr: true,
			err:     context.Canceled,
		},
		{
			name: "success in uploading files",
			setUpMock: func() {
				rootContext, rootContextCancel = context.WithCancel(context.Background())
				fileListChan := make(chan *s3.ObjectMeta, 1000)
				fileListChan <- &s3.ObjectMeta{
					Key: "file1", Size: 1200,
				}

				fileListChan <- &s3.ObjectMeta{
					Key: "file2", Size: 1400,
				}

				fileListChan <- &s3.ObjectMeta{
					Key: "file3", Size: 1500,
				}

				close(fileListChan)

				errChan := make(chan error, 1)
				close(errChan)
				awsStorageService.EXPECT().ListFilesInBucket(gomock.Any()).Return(fileListChan, errChan)

				updateStateKeyFunc = func(statePath string) (func(stateKey string), func(), error) {
					return func(stateKey string) {}, func() {}, nil
				}

				dStorageService.EXPECT().UpdateAllocationDetails().Return(nil)
				dStorageService.EXPECT().GetAvailableSpace().Return(int64(4200))
				dStorageService.EXPECT().IsFileExist(gomock.Any(), gomock.Any()).AnyTimes().Return(false, nil)
				awsStorageService.EXPECT().GetFileContent(gomock.Any(), "file1").Return(&s3.Object{}, nil)
				awsStorageService.EXPECT().GetFileContent(gomock.Any(), "file2").Return(&s3.Object{}, nil)
				awsStorageService.EXPECT().GetFileContent(gomock.Any(), "file3").Return(&s3.Object{}, nil)
				dStorageService.EXPECT().Upload(gomock.Any(), "file1", gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
				dStorageService.EXPECT().Upload(gomock.Any(), "file2", gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
				dStorageService.EXPECT().Upload(gomock.Any(), "file3", gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
			},
			wantErr: false,
		},
		{
			name: "aws get content error",
			setUpMock: func() {
				rootContext, rootContextCancel = context.WithCancel(context.Background())
				fileListChan := make(chan *s3.ObjectMeta, 1000)
				fileListChan <- &s3.ObjectMeta{
					Key: "file11", Size: 1200,
				}

				fileListChan <- &s3.ObjectMeta{
					Key: "file22", Size: 1400,
				}

				fileListChan <- &s3.ObjectMeta{
					Key: "file33", Size: 1500,
				}

				close(fileListChan)

				errChan := make(chan error, 1)
				close(errChan)
				awsStorageService.EXPECT().ListFilesInBucket(gomock.Any()).Return(fileListChan, errChan)

				updateStateKeyFunc = func(statePath string) (func(stateKey string), func(), error) {
					return func(stateKey string) {}, func() {}, nil
				}

				dStorageService.EXPECT().UpdateAllocationDetails().Return(nil)
				dStorageService.EXPECT().GetAvailableSpace().Return(int64(4200))
				dStorageService.EXPECT().IsFileExist(gomock.Any(), gomock.Any()).AnyTimes().Return(false, nil)
				awsStorageService.EXPECT().GetFileContent(gomock.Any(), "file11").AnyTimes().Return(&s3.Object{}, nil)
				awsStorageService.EXPECT().GetFileContent(gomock.Any(), "file33").AnyTimes().Return(&s3.Object{}, nil)
				awsStorageService.EXPECT().GetFileContent(gomock.Any(), "file22").Return(&s3.Object{}, errors.New("some error"))
				dStorageService.EXPECT().Upload(gomock.Any(), "file11", gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
				dStorageService.EXPECT().Upload(gomock.Any(), "file22", gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
				dStorageService.EXPECT().Upload(gomock.Any(), "file33", gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
			},
			wantErr: true,
			err:     context.Canceled,
		},
		{
			name: "dstorage upload error",
			setUpMock: func() {
				rootContext, rootContextCancel = context.WithCancel(context.Background())
				fileListChan := make(chan *s3.ObjectMeta, 1000)
				fileListChan <- &s3.ObjectMeta{
					Key: "file10", Size: 1200,
				}

				fileListChan <- &s3.ObjectMeta{
					Key: "file20", Size: 1400,
				}

				fileListChan <- &s3.ObjectMeta{
					Key: "file30", Size: 1500,
				}

				close(fileListChan)

				errChan := make(chan error, 1)
				close(errChan)
				awsStorageService.EXPECT().ListFilesInBucket(gomock.Any()).Return(fileListChan, errChan)

				updateStateKeyFunc = func(statePath string) (func(stateKey string), func(), error) {
					return func(stateKey string) {}, func() {}, nil
				}

				dStorageService.EXPECT().UpdateAllocationDetails().Return(nil)
				dStorageService.EXPECT().GetAvailableSpace().Return(int64(4200))
				dStorageService.EXPECT().IsFileExist(gomock.Any(), gomock.Any()).AnyTimes().Return(false, nil)
				awsStorageService.EXPECT().GetFileContent(gomock.Any(), "file10").Return(&s3.Object{}, nil)
				awsStorageService.EXPECT().GetFileContent(gomock.Any(), "file20").Return(&s3.Object{}, nil)
				awsStorageService.EXPECT().GetFileContent(gomock.Any(), "file30").Return(&s3.Object{}, nil)
				dStorageService.EXPECT().Upload(gomock.Any(), "file10", gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
				dStorageService.EXPECT().Upload(gomock.Any(), "file20", gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("some error"))
				dStorageService.EXPECT().Upload(gomock.Any(), "file30", gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
			},
			wantErr: true,
			err:     context.Canceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setUpMock()
			err := Migrate()
			log.Println(err)
			if tt.wantErr != (err != nil) {
				t.Errorf("s3-migration Migrate, wantErr: %v, got: %v", tt.wantErr, err)
			}
			if err != nil && err.Error() != tt.err.Error() {
				t.Errorf("s3-migration Migrate, want errMsg: %v, got: %v", tt.err, err)
			}
		})
	}
}
