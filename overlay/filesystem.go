// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

// +build linux darwin

package overlay

import (
	"log"
	"sync"
	"syscall"
	"time"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"golang.org/x/net/context"
)

// FS is the filesystem root
type FS struct {
	rootPath string

	xlock  sync.RWMutex
	xattrs map[string]map[string][]byte

	nlock sync.Mutex
	nodes map[string][]*Node // realPath -> nodes

	latency time.Duration
}

func NewFS(latency time.Duration) *FS {
	return &FS{
		rootPath: ".",
		xattrs:   make(map[string]map[string][]byte),
		nodes:    make(map[string][]*Node),
		latency:  latency,
	}
}

func (f *FS) newNode(n *Node) {
	rp := n.getRealPath()

	f.nlock.Lock()
	defer f.nlock.Unlock()
	f.nodes[rp] = append(f.nodes[rp], n)
}

func (f *FS) nodeRenamed(oldPath string, newPath string) {
	f.nlock.Lock()
	defer f.nlock.Unlock()
	f.nodes[newPath] = append(f.nodes[newPath], f.nodes[oldPath]...)
	delete(f.nodes, oldPath)
	for _, n := range f.nodes[newPath] {
		n.updateRealPath(newPath)
	}
}

func (f *FS) forgetNode(n *Node) {
	f.nlock.Lock()
	defer f.nlock.Unlock()
	nodes, ok := f.nodes[n.realPath]
	if !ok {
		return
	}

	found := -1
	for i, node := range nodes {
		if node == n {
			found = i
			break
		}
	}

	if found > -1 {
		nodes = append(nodes[:found], nodes[found+1:]...)
	}
	if len(nodes) == 0 {
		delete(f.nodes, n.realPath)
	} else {
		f.nodes[n.realPath] = nodes
	}
}

// Root implements fs.FS interface for *FS
func (f *FS) Root() (n fs.Node, err error) {
	time.Sleep(f.latency)
	defer func() { log.Printf("FS.Root(): %#+v error=%v", n, err) }()
	nn := &Node{realPath: f.rootPath, isDir: true, fs: f}
	f.newNode(nn)
	return nn, nil
}

var _ fs.FSStatfser = (*FS)(nil)

// Statfs implements fs.FSStatfser interface for *FS
func (f *FS) Statfs(ctx context.Context,
	req *fuse.StatfsRequest, resp *fuse.StatfsResponse) (err error) {
	time.Sleep(f.latency)
	defer func() { log.Printf("FS.Statfs(): error=%v", err) }()
	var stat syscall.Statfs_t
	if err := syscall.Statfs(f.rootPath, &stat); err != nil {
		return translateError(err)
	}
	resp.Blocks = stat.Blocks
	resp.Bfree = stat.Bfree
	resp.Bavail = stat.Bavail
	resp.Files = 0 // TODO
	resp.Ffree = stat.Ffree
	resp.Bsize = uint32(stat.Bsize)
	resp.Namelen = 255 // TODO
	resp.Frsize = 8    // TODO

	return nil
}

// if to is empty, all xattrs on the node is removed
func (f *FS) moveAllxattrs(ctx context.Context, from string, to string) {
	f.xlock.Lock()
	defer f.xlock.Unlock()
	if f.xattrs[from] != nil {
		if to != "" {
			f.xattrs[to] = f.xattrs[from]
		}
		f.xattrs[from] = nil
	}
}
