package migration

import (
	"context"
	"errors"
	"log"
	"testing"

	T "github.com/0chain/s3migration/types"

	mock_dstorage "github.com/0chain/s3migration/dstorage/mocks"
	mock_s3 "github.com/0chain/s3migration/s3/mocks"
	mock_util "github.com/0chain/s3migration/util/mocks"
	"github.com/golang/mock/gomock"
)

func TestMigrate(t *testing.T) {

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	dStorageService := mock_dstorage.NewMockDStoreI(ctrl)
	awsStorageService := mock_s3.NewMockAwsI(ctrl)
	fileSystem := mock_util.NewMockFileSystem(ctrl)
	// migration = Migration{
	// 	zStore:   dStorageService,
	// 	awsStore: awsStorageService,
	// 	skip:     Skip,
	// 	fs:       fileSystem,
	// }

	tests := []struct {
		name             string
		setUpMock        func()
		wantErr          bool
		err              error
		migrateFileCount int
	}{
		{
			name: "success in uploading files",
			setUpMock: func() {
				rootContext, rootContextCancel = context.WithCancel(context.Background())
				fileListChan := make(chan *T.ObjectMeta, 1000)
				fileListChan <- &T.ObjectMeta{
					Key: "file1", Size: 1200,
				}

				fileListChan <- &T.ObjectMeta{
					Key: "file2", Size: 1400,
				}

				fileListChan <- &T.ObjectMeta{
					Key: "file3", Size: 1500,
				}

				close(fileListChan)

				errChan := make(chan error, 1)
				close(errChan)
				awsStorageService.EXPECT().ListFiles(gomock.Any()).Return(fileListChan, errChan)

				awsStorageService.EXPECT().DownloadToFile(gomock.Any(), "file1").Return("/aws/file1", nil)
				awsStorageService.EXPECT().DownloadToFile(gomock.Any(), "file2").Return("/aws/file2", nil)
				awsStorageService.EXPECT().DownloadToFile(gomock.Any(), "file3").Return("/aws/file3", nil)

				updateKeyFunc = func(statePath string) (func(stateKey string), func(), error) {
					return func(stateKey string) {}, func() {}, nil
				}

				dStorageService.EXPECT().IsFileExist(gomock.Any(), getRemotePath("file1")).Return(false, nil)
				dStorageService.EXPECT().IsFileExist(gomock.Any(), getRemotePath("file2")).Return(false, nil)
				dStorageService.EXPECT().IsFileExist(gomock.Any(), getRemotePath("file3")).Return(false, nil)

				fileInfo := mock_util.NewMockFileInfo(ctrl)
				file1Data := mock_util.NewMockFile(ctrl)
				file1Data.EXPECT().Stat().Return(fileInfo, nil)
				file1Data.EXPECT().Read(gomock.Any()).Return(1, nil)
				file1Data.EXPECT().Seek(gomock.Any(), gomock.Any()).Return(int64(123), nil)
				file1Data.EXPECT().Close().Return(nil)
				file2Data := mock_util.NewMockFile(ctrl)
				file2Data.EXPECT().Stat().Return(fileInfo, nil)
				file2Data.EXPECT().Read(gomock.Any()).Return(1, nil)
				file2Data.EXPECT().Seek(gomock.Any(), gomock.Any()).Return(int64(123), nil)
				file2Data.EXPECT().Close().Return(nil)
				file3Data := mock_util.NewMockFile(ctrl)
				file3Data.EXPECT().Stat().Return(fileInfo, nil)
				file3Data.EXPECT().Read(gomock.Any()).Return(1, nil)
				file3Data.EXPECT().Seek(gomock.Any(), gomock.Any()).Return(int64(123), nil)
				fileInfo.EXPECT().Size().AnyTimes().Return(int64(122))
				file3Data.EXPECT().Close().Return(nil)

				fileSystem.EXPECT().Open("/aws/file1").Return(file1Data, nil)
				fileSystem.EXPECT().Open("/aws/file2").Return(file2Data, nil)
				fileSystem.EXPECT().Open("/aws/file3").Return(file3Data, nil)

				fileSystem.EXPECT().Remove("/aws/file1").Return(nil)
				fileSystem.EXPECT().Remove("/aws/file2").Return(nil)
				fileSystem.EXPECT().Remove("/aws/file3").Return(nil)

				dStorageService.EXPECT().Upload(gomock.Any(), getRemotePath("file1"), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
				dStorageService.EXPECT().Upload(gomock.Any(), getRemotePath("file2"), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
				dStorageService.EXPECT().Upload(gomock.Any(), getRemotePath("file3"), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
			},
			wantErr: false,
		},
		{
			name: "download to file error",
			setUpMock: func() {
				rootContext, rootContextCancel = context.WithCancel(context.Background())
				fileListChan := make(chan *T.ObjectMeta, 1000)
				fileListChan <- &T.ObjectMeta{
					Key: "file11", Size: 1200,
				}

				close(fileListChan)

				errChan := make(chan error, 1)
				close(errChan)
				awsStorageService.EXPECT().ListFiles(gomock.Any()).Return(fileListChan, errChan)

				updateKeyFunc = func(statePath string) (func(stateKey string), func(), error) {
					return func(stateKey string) {}, func() {}, nil
				}

				dStorageService.EXPECT().IsFileExist(gomock.Any(), getRemotePath("file11")).Return(false, nil)

				awsStorageService.EXPECT().DownloadToFile(gomock.Any(), "file11").AnyTimes().Return("", errors.New("some error"))
			},
			wantErr: true,
			err:     errors.New("some error"),
		},
		{
			name: "dstorage upload error",
			setUpMock: func() {
				rootContext, rootContextCancel = context.WithCancel(context.Background())
				fileListChan := make(chan *T.ObjectMeta, 1000)
				fileListChan <- &T.ObjectMeta{
					Key: "file10", Size: 1200,
				}

				close(fileListChan)

				errChan := make(chan error, 1)
				close(errChan)
				awsStorageService.EXPECT().ListFiles(gomock.Any()).Return(fileListChan, errChan)

				updateKeyFunc = func(statePath string) (func(stateKey string), func(), error) {
					return func(stateKey string) {}, func() {}, nil
				}

				dStorageService.EXPECT().IsFileExist(gomock.Any(), "file10").AnyTimes().Return(false, nil)
				awsStorageService.EXPECT().DownloadToFile(gomock.Any(), "file10").Return("/aws/file10", nil)

				fileInfo := mock_util.NewMockFileInfo(ctrl)
				file1Data := mock_util.NewMockFile(ctrl)
				file1Data.EXPECT().Stat().Return(fileInfo, nil).Times(3)
				file1Data.EXPECT().Read(gomock.Any()).Return(1, nil).Times(3)
				file1Data.EXPECT().Seek(gomock.Any(), gomock.Any()).Return(int64(123), nil).Times(3)
				file1Data.EXPECT().Close().Return(nil).Times(3)

				fileSystem.EXPECT().Open("/aws/file10").Return(file1Data, nil).Times(3)
				fileSystem.EXPECT().Remove("/aws/file10").Return(nil)

				fileInfo.EXPECT().Size().AnyTimes().Return(int64(122))

				dStorageService.EXPECT().Upload(gomock.Any(), "file10", gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("some error")).Times(3)
			},
			wantErr: true,
			err:     errors.New("after 3 attempts, last error: some error"),
		},
		{
			name: "aws list object error",
			setUpMock: func() {
				rootContext, rootContextCancel = context.WithCancel(context.Background())
				fileListChan := make(chan *T.ObjectMeta, 1000)

				close(fileListChan)

				errChan := make(chan error, 1)
				errChan <- errors.New("some error")

				awsStorageService.EXPECT().ListFiles(gomock.Any()).Return(fileListChan, errChan)

				updateKeyFunc = func(statePath string) (func(stateKey string), func(), error) {
					return func(stateKey string) {}, func() {}, nil
				}
			},
			wantErr: true,
			err:     errors.New("some error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setUpMock()
			err := StartMigration()
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
