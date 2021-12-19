package dStorage

import (
	"fmt"
	"github.com/0chain/gosdk/core/transaction"
	"os"
	"sync"
)

type StatusCB struct {
	wg      *sync.WaitGroup
	success bool
}

func (s *StatusCB) Started(allocationId, filePath string, op int, totalBytes int) {
}

func (s *StatusCB) InProgress(allocationId, filePath string, op int, completedBytes int, data []byte) {
}

func (s *StatusCB) Error(allocationID string, filePath string, op int, err error) {
	s.success = false
	defer s.wg.Done()

	var errDetail interface{} = "Unknown Error"
	if err != nil {
		errDetail = err.Error()
	}

	PrintError("Error in file operation:", errDetail)
}

func (s *StatusCB) Completed(allocationId, filePath string, filename string, mimetype string, size int, op int) {
	s.success = true
	defer s.wg.Done()
	fmt.Println("Status completed callback. Type = " + mimetype + ". Name = " + filename)
}

func (s *StatusCB) CommitMetaCompleted(request, response string, txn *transaction.Transaction, err error) {
	defer s.wg.Done()
	if err != nil {
		s.success = false
		PrintError("Error in commitMetaTransaction." + err.Error())
	} else {
		s.success = true
		fmt.Println("Commit Metadata successful, Response :", response)
	}
}

func (s *StatusCB) RepairCompleted(filesRepaired int) {
	defer s.wg.Done()
	fmt.Println("Repair file completed, Total files repaired: ", filesRepaired)
}

func PrintError(v ...interface{}) {
	fmt.Fprintln(os.Stderr, v...)
}
