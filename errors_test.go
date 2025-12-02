package statelog

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestStatelogIOErrorMessage(t *testing.T) {
	fileOp := "O_RDONLY"
	filePath := "/home/test/test.statelog"
	err := StatelogIOError{
		Op:       fileOp,
		FilePath: filePath,
		Err:      os.ErrNotExist,
	}
	errMsg := err.Error()

	if !strings.Contains(errMsg, fileOp) {
		t.Errorf("error message should contain file operation, got: %s", errMsg)
	}

	if !strings.Contains(errMsg, filePath) {
		t.Errorf("error message should contain file path, got: %s", errMsg)
	}
}

func TestStatelogIOErrorUwrap(t *testing.T) {
	err := StatelogIOError{
		Op:       "O_WRONLY",
		FilePath: "/var/log/index.statelog",
		Err:      os.ErrPermission,
	}
	underlyingErr := err.Unwrap()

	if !errors.Is(underlyingErr, os.ErrPermission) {
		t.Errorf("expected underlying error to be ErrPermission, got: %v", underlyingErr)
	}
}
