// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

// Keybase file system

package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/keybase/kbfs/libwebdav"
	"golang.org/x/net/webdav"
)

func log(req *http.Request, err error) {
	fmt.Printf("Req=%v, err=%v\n", req, err)
}

func main() {
	if len(os.Args) != 2 {
		fmt.Println("Must be called with a directory, like: ./kbfswebdav /keybase/public/chris")
		os.Exit(-1)
	}
	h := &webdav.Handler{
		FileSystem: webdav.Dir(os.Args[1]),
		LockSystem: &libwebdav.Locker{},
		Logger:     log,
	}
	//then use the Handler.ServeHTTP Method as the http.HandleFunc
	http.HandleFunc("/", h.ServeHTTP)
	http.ListenAndServe(":5555", nil)
}
