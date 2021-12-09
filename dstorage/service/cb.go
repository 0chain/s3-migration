package service

import (
	"fmt"
	"os"
	"sync"

	"github.com/0chain/gosdk/zboxcore/sdk"
	"gopkg.in/cheggaaa/pb.v1"
)

func (s *StatusBar) Started(allocationId, filePath string, op int, totalBytes int) {
	s.b = pb.StartNew(totalBytes)
	s.b.Set(0)
}
func (s *StatusBar) InProgress(allocationId, filePath string, op int, completedBytes int, data []byte) {
	s.b.Set(completedBytes)
}

func (s *StatusBar) Completed(allocationId, filePath string, filename string, mimetype string, size int, op int) {
	if s.b != nil {
		s.b.Finish()
	}
	s.Success = true
	s.Wg.Done()
}

func (s *StatusBar) Error(allocationID string, filePath string, op int, err error) {
	if s.b != nil {
		s.b.Finish()
	}
	s.Success = false
	s.Wg.Done()
	var errDetail interface{} = "Unknown Error"
	if err != nil {
		errDetail = err.Error()
	}

	PrintError("Error in file operation:", errDetail)
}

func (s *StatusBar) CommitMetaCompleted(request, response string, err error) {
	defer s.Wg.Done()
	if err != nil {
		s.Success = false
		PrintError("Error in commitMetaTransaction." + err.Error())
	} else {
		s.Success = true
		fmt.Println("Commit Metadata successful, Response :", response)
	}
}

func (s *StatusBar) RepairCompleted(filesRepaired int) {
}

type StatusBar struct {
	b       *pb.ProgressBar
	Wg      *sync.WaitGroup
	Success bool
}

func PrintError(v ...interface{}) {
	fmt.Fprintln(os.Stderr, v...)
}

func PrintInfo(v ...interface{}) {
	fmt.Fprintln(os.Stdin, v...)
}

func commitMetaTxn(path, crudOp, authTicket, lookupHash string, a *sdk.Allocation, fileMeta *sdk.ConsolidatedFileMeta, status *StatusBar) {
	err := a.CommitMetaTransaction(path, crudOp, authTicket, lookupHash, fileMeta, status)
	if err != nil {
		PrintError("Commit failed.", err)
		os.Exit(1)
	}
}
