package dStorage

import "github.com/0chain/gosdk/core/transaction"

type StatusCB struct {
	DoneCh chan struct{}
	ErrCh  chan error
}

func (cb *StatusCB) Started(allocationId, filePath string, op int, totalBytes int) {

}

func (cb *StatusCB) InProgress(allocationId, filePath string, op int, completedBytes int, data []byte) {

}

func (cb *StatusCB) Error(allocationID string, filePath string, op int, err error) {

}

func (cb *StatusCB) Completed(allocationId, filePath string, filename string, mimetype string, size int, op int) {

}

func (cb *StatusCB) CommitMetaCompleted(request, response string, txn *transaction.Transaction, err error) {

}

func (cb *StatusCB) RepairCompleted(filesRepaired int) {

}
