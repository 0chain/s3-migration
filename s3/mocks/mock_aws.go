// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/0chain/s3migration/s3 (interfaces: AwsI)

// Package mock_s3 is a generated GoMock package.
package mock_s3

import (
	context "context"
	reflect "reflect"

	s3 "github.com/0chain/s3migration/s3"
	gomock "github.com/golang/mock/gomock"
)

// MockAwsI is a mock of AwsI interface.
type MockAwsI struct {
	ctrl     *gomock.Controller
	recorder *MockAwsIMockRecorder
}

// MockAwsIMockRecorder is the mock recorder for MockAwsI.
type MockAwsIMockRecorder struct {
	mock *MockAwsI
}

// NewMockAwsI creates a new mock instance.
func NewMockAwsI(ctrl *gomock.Controller) *MockAwsI {
	mock := &MockAwsI{ctrl: ctrl}
	mock.recorder = &MockAwsIMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockAwsI) EXPECT() *MockAwsIMockRecorder {
	return m.recorder
}

// DeleteFile mocks base method.
func (m *MockAwsI) DeleteFile(arg0 context.Context, arg1 string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DeleteFile", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// DeleteFile indicates an expected call of DeleteFile.
func (mr *MockAwsIMockRecorder) DeleteFile(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeleteFile", reflect.TypeOf((*MockAwsI)(nil).DeleteFile), arg0, arg1)
}

// DownloadToFile mocks base method.
func (m *MockAwsI) DownloadToFile(arg0 context.Context, arg1 string) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DownloadToFile", arg0, arg1)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// DownloadToFile indicates an expected call of DownloadToFile.
func (mr *MockAwsIMockRecorder) DownloadToFile(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DownloadToFile", reflect.TypeOf((*MockAwsI)(nil).DownloadToFile), arg0, arg1)
}

// GetFileContent mocks base method.
func (m *MockAwsI) GetFileContent(arg0 context.Context, arg1 string) (*s3.Object, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetFileContent", arg0, arg1)
	ret0, _ := ret[0].(*s3.Object)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetFileContent indicates an expected call of GetFileContent.
func (mr *MockAwsIMockRecorder) GetFileContent(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetFileContent", reflect.TypeOf((*MockAwsI)(nil).GetFileContent), arg0, arg1)
}

// ListFilesInBucket mocks base method.
func (m *MockAwsI) ListFilesInBucket(arg0 context.Context) (<-chan *s3.ObjectMeta, <-chan error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ListFilesInBucket", arg0)
	ret0, _ := ret[0].(<-chan *s3.ObjectMeta)
	ret1, _ := ret[1].(<-chan error)
	return ret0, ret1
}

// ListFilesInBucket indicates an expected call of ListFilesInBucket.
func (mr *MockAwsIMockRecorder) ListFilesInBucket(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ListFilesInBucket", reflect.TypeOf((*MockAwsI)(nil).ListFilesInBucket), arg0)
}
