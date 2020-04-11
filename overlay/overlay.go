// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

// +build linux darwin

package overlay

import (
	"log"
	"os"
	"syscall"
	"time"

	"bazil.org/fuse"
	"github.com/pkg/xattr"
)

const (
	attrValidDuration = time.Second
)

func translateError(err error) error {
	switch {
	case os.IsNotExist(err):
		return fuse.ENOENT
	case os.IsExist(err):
		return fuse.EEXIST
	case os.IsPermission(err):
		return fuse.EPERM
	default:
		return err
	}
}

// unpackSysErr unpacks the underlying syscall.Errno from an error value
// returned by Get/Set/...
func unpackSysErr(err error) syscall.Errno {
	if err == nil {
		return syscall.Errno(0)
	}
	err2, ok := err.(*xattr.Error)
	if !ok {
		log.Panicf("cannot unpack err=%#v", err)
	}
	return err2.Err.(syscall.Errno)
}
