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
	"time"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"

	"github.com/butonic/ocis-overlay/overlay"
)

var (
	latency time.Duration
)

func init() {
	flag.DurationVar(&latency, "latency", 0,
		"add an artificial latency to every fuse handler on every call")
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s ROOT\n", os.Args[0])
	flag.PrintDefaults()
}

func main() {
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() != 1 {
		usage()
		os.Exit(2)
	}
	mountpoint := flag.Arg(0)

	if err := os.Chdir(mountpoint); err != nil {
		log.Fatal(err)
	}

	log.Println("changed into dir!")

	c, err := fuse.Mount(
		".",
		fuse.FSName("ocis-overlay"),
		fuse.Subtype("ocis-overlay-fs"),
		fuse.VolumeName("OCISOverlay"),
		fuse.AllowNonEmptyMount(),
		fuse.AllowOther(),
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

	err = fs.Serve(c, overlay.NewFS(
		latency,
	))
	if err != nil {
		log.Fatal(err)
	}

}
