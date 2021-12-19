package errors

import (
	zerror "github.com/0chain/errors"
)

const (
	FileNoExistErrCode                 = "file_no_exist"
	FileExistErrCode                   = "file_exist"
	ConsensusFailedErrCode             = "consensus_failed"
	TransactionValidationFailedErrCode = "transaction_validation_failed"
)

var (
	ErrFileNoExist           = zerror.New(FileExistErrCode, "")
	ErrFileExist             = zerror.New(FileExistErrCode, "")
	ErrConsensusFailed       = zerror.New(ConsensusFailedErrCode, "")
	ErrTransactionValidation = zerror.New(TransactionValidationFailedErrCode, "")
)

func IsConsensusFailedError(err error) bool {
	if err == nil {
		return false
	}

	switch err := err.(type) {
	case *zerror.Error:
		if err.Code == ConsensusFailedErrCode {
			return true
		}
	}
	return false
}
