package libkbfs

import "os"

// This file is a workaround for
// https://github.com/golang/go/issues/17164 .

func isExist(err error) bool {
	return os.IsExist(err)
}
