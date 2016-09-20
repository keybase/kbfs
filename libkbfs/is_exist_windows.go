package libkbfs

import (
	"os"
	"syscall"
)

// This file is a workaround for
// https://github.com/golang/go/issues/17164 .

const _ERROR_DIR_NOT_EMPTY = syscall.Errno(145)

func isExist(err error) bool {
	return os.IsExist(err) || err == _ERROR_DIR_NOT_EMPTY
}
