package dStorage

import (
	"fmt"
	// "log"
	// "os"
	"sync"

	zlogger "github.com/0chain/s3migration/logger"
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
	s.success = true
	// if migration.deleteSource {
	// 	if err := migration.awsStore.DeleteFile(ctx, downloadObj.ObjectKey); err != nil {
	// 		zlogger.Logger.Error(err)
	// 		dsFileHandler.Write([]byte(downloadObj.ObjectKey + "\n"))
	// 	}
	// }
	// migration.szCtMu.Lock()
	// migration.migratedSize += uint64(downloadObj.Size)
	// migration.totalMigratedObjects++
	// migration.szCtMu.Unlock()
	defer s.wg.Done()

	fmt.Println("Status completed callback. Type = " + mimetype + ". Name = " + filename)
}

func (s *StatusBar) Error(allocationID string, filePath string, op int, err error) {
	if s.b != nil {
		s.b.Finish()
	}
	s.success = false
	defer s.wg.Done()

	var errDetail interface{} = "Unknown Error"
	if err != nil {
		errDetail = err.Error()
	}

	zlogger.Logger.Error("Error in file operation:", errDetail)
}

func (s *StatusBar) RepairCompleted(filesRepaired int) {

}

type StatusBar struct {
	b       *pb.ProgressBar
	wg      *sync.WaitGroup
	success bool
}
