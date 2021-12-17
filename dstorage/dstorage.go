package dStorage

import (
	"context"
	"io"

	"github.com/0chain/gosdk/core/common"
	"github.com/0chain/gosdk/zboxcore/sdk"
)

//use rate limiter here.
//All upload should go through this file so you can limit rate of upload request so you don't get blocked by blobber.
//Its better to put rate limit value in some variable; check rate limit of all blobbers and put rate limit value of the blobber that has minimum capacity.
//While this file helps to rate limit; there might be goroutine leak in migrate.go so that we need to process uploads in batch.
//
//Batch is simpler to use than the continuous upload.
//Concept is you take a bunch of s3 objects in batch and wait until all the objects from this batch is uploaded. If any upload fails then terminate migration of this bucket.
//let other bucket operate.
//This way you can update state for each bucket;

//We also need to be careful about committing upload. There might be race between committing request resulting in commit failure.
//So lets put commit request in a queue(use channel) and try three times. If it fails to commit then save state of all bucket and abort the program.

type DStoreI interface {
	GetFileMetaData(ctx context.Context, remotepath string) (*sdk.ORef, error)
	Replace(ctx context.Context, remotepath string, r io.Reader, size int64) error
	Duplicate(ctx context.Context, remotepath string, r io.Reader, size int64) error
	Upload(ctx context.Context, remotepath string, r io.Reader, size int64) error
	IsFileExist(ctx context.Context, remotepath string) bool
	GetAvailableSpace() uint64
	GetTotalSpace() uint64
}

type DStorageService struct {
	allocation *sdk.Allocation
	encrypt    bool // Should encrypt before uploading/updating
	//After file is available in dStorage owner can decide who is going to pay for read
	whoPays common.WhoPays
	//Where to migrate all buckets to. Default is /
	migrateTo string
	//Duplicate suffix to use if file already exists in dStorage. So if remotepath if /path/to/remote/file.txt
	//then duplicate path should be /path/to/remote/file{duplicateSuffix}.txt
	duplicateSuffix string
	availableSpace  uint64
	totalSpace      uint64
}

func (d *DStorageService) GetFileMetaData(ctx context.Context, remotepath string) (*sdk.ORef, error) {
	//if error is nil and ref too is nil then it means remoepath does not exist.
	//in this case return error with code from error.go
	return nil, nil
}

func (d *DStorageService) Upload(ctx context.Context, remotepath string, r io.Reader, size int64) error {
	return nil
}

func (d *DStorageService) Replace(ctx context.Context, remotepath string, r io.Reader, size int64) error {
	return nil
}

func (d *DStorageService) Duplicate(ctx context.Context, remotepath string, r io.Reader, size int64) error {
	_ = d.duplicateSuffix
	return nil
}

func (d *DStorageService) IsFileExist(ctx context.Context, remotepath string) bool {
	return false
}

func (d *DStorageService) GetAvailableSpace() uint64 {
	// allocationdetails := getallocationdetailsfrom0chain()
	return d.availableSpace
}

func (d *DStorageService) GetTotalSpace() uint64 {
	return d.totalSpace
}

func GetDStorageService(allocationID, migrateTo, duplicateSuffix string, encrypt bool, whoPays int) (*DStorageService, error) {
	allocation, err := sdk.GetAllocation(allocationID)

	if err != nil {
		return nil, err
	}

	return &DStorageService{
		allocation: allocation,
		encrypt:    encrypt,
		whoPays:    common.WhoPays(whoPays),
		migrateTo:  migrateTo,
	}, nil
}
