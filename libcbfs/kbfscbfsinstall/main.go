// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

// Keybase file system

package main

import (
	"log"

	"github.com/keybase/kbfs/libcbfs/cbfs"
)

func main() {
	log.Println("Starting to install cbfs from cbfs.cab")
	err := cbfs.Install("kbfscbfs", "cbfs.cab")
	if err != nil {
		log.Fatal("Error: ", err)
	}
	log.Println("ok")
}
