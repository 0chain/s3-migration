package errors

import (
	zerror "github.com/0chain/errors"
)

const (
	FileNoExistErrCode                 = "file_no_exist"
	FileExistErrCode                   = "file_exist"
	ConsensusFailedErrCode             = "consensus_failed"
	TransactionValidationFailedErrCode = "transaction_validation_failed"
	InsufficientZStorageSpace          = "insufficient_storage_space"
)

var (
	ErrFileNoExist           = zerror.New(FileNoExistErrCode, "")
	ErrFileExist             = zerror.New(FileExistErrCode, "")
	ErrConsensusFailed       = zerror.New(ConsensusFailedErrCode, "")
	ErrTransactionValidation = zerror.New(TransactionValidationFailedErrCode, "")
	ErrInsufficientSpace     = zerror.New(InsufficientZStorageSpace, "")
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

func IsFileNotExistError(err error) bool {
	if err == nil {
		return false
	}

	switch err := err.(type) {
	case *zerror.Error:
		if err.Code == FileNoExistErrCode {
			return true
		}
	}
	return false
}
