// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

// +build linux darwin

package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
)

func usage() {
	fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s ROOT MOUNTPOINT\n", os.Args[0])
	flag.PrintDefaults()
}

func main() {
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() != 2 {
		usage()
		os.Exit(2)
	}
	mountpoint := flag.Arg(1)

	c, err := fuse.Mount(
		mountpoint,
		fuse.FSName("loopback"),
		fuse.Subtype("loopback-fs"),
		fuse.VolumeName("Loopback"),
		fuse.AllowRoot(),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	// check if the mount process has an error to report
	<-c.Ready
	if err := c.MountError; err != nil {
		log.Fatal(err)
	}

	log.Println("mounted!")

	err = fs.Serve(c, &FS{rootPath: flag.Arg(0)})
	if err != nil {
		log.Fatal(err)
	}

}
