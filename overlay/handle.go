// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

// +build linux darwin

package overlay

import (
	"io/ioutil"
	"log"
	"os"
	"time"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"golang.org/x/net/context"
)

// Handle represent an open file or directory
type Handle struct {
	fs        *FS
	reopener  func() (*os.File, error)
	forgetter func()

	f *os.File
}

var _ fs.HandleFlusher = (*Handle)(nil)

// Flush implements fs.HandleFlusher interface for *Handle
func (h *Handle) Flush(ctx context.Context,
	req *fuse.FlushRequest) (err error) {
	time.Sleep(h.fs.latency)
	defer func() { log.Printf("Handle(%s).Flush(): error=%v", h.f.Name(), err) }()
	return h.f.Sync()
}

var _ fs.HandleReadAller = (*Handle)(nil)

// ReadAll implements fs.HandleReadAller interface for *Handle
func (h *Handle) ReadAll(ctx context.Context) (d []byte, err error) {
	time.Sleep(h.fs.latency)
	defer func() {
		log.Printf("Handle(%s).ReadAll(): error=%v",
			h.f.Name(), err)
	}()
	return ioutil.ReadAll(h.f)
}

var _ fs.HandleReadDirAller = (*Handle)(nil)

// ReadDirAll implements fs.HandleReadDirAller interface for *Handle
func (h *Handle) ReadDirAll(ctx context.Context) (
	dirs []fuse.Dirent, err error) {
	time.Sleep(h.fs.latency)
	defer func() {
		log.Printf("Handle(%s).ReadDirAll(): %#+v error=%v",
			h.f.Name(), dirs, err)
	}()
	fis, err := h.f.Readdir(0)
	if err != nil {
		return nil, translateError(err)
	}

	// Readdir() reads up the entire dir stream but never resets the pointer.
	// Consequently, when Readdir is called again on the same *File, it gets
	// nothing. As a result, we need to close the file descriptor and re-open it
	// so next call would work.
	if err = h.f.Close(); err != nil {
		return nil, translateError(err)
	}
	if h.f, err = h.reopener(); err != nil {
		return nil, translateError(err)
	}

	return getDirentsWithFileInfos(fis), nil
}

var _ fs.HandleReader = (*Handle)(nil)

// Read implements fs.HandleReader interface for *Handle
func (h *Handle) Read(ctx context.Context,
	req *fuse.ReadRequest, resp *fuse.ReadResponse) (err error) {
	time.Sleep(h.fs.latency)
	defer func() {
		log.Printf("Handle(%s).Read(): error=%v",
			h.f.Name(), err)
	}()

	if _, err = h.f.Seek(req.Offset, 0); err != nil {
		return translateError(err)
	}
	resp.Data = make([]byte, req.Size)
	n, err := h.f.Read(resp.Data)
	resp.Data = resp.Data[:n]
	return translateError(err)
}

var _ fs.HandleReleaser = (*Handle)(nil)

// Release implements fs.HandleReleaser interface for *Handle
func (h *Handle) Release(ctx context.Context,
	req *fuse.ReleaseRequest) (err error) {
	time.Sleep(h.fs.latency)
	defer func() {
		log.Printf("Handle(%s).Release(): error=%v",
			h.f.Name(), err)
	}()
	if h.forgetter != nil {
		h.forgetter()
	}
	return h.f.Close()
}

var _ fs.HandleWriter = (*Handle)(nil)

// Write implements fs.HandleWriter interface for *Handle
func (h *Handle) Write(ctx context.Context,
	req *fuse.WriteRequest, resp *fuse.WriteResponse) (err error) {
	time.Sleep(h.fs.latency)
	defer func() {
		log.Printf("Handle(%s).Write(): error=%v",
			h.f.Name(), err)
	}()

	if _, err = h.f.Seek(req.Offset, 0); err != nil {
		return translateError(err)
	}
	n, err := h.f.Write(req.Data)
	resp.Size = n
	return translateError(err)
}
