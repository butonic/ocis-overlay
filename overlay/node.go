// Copyright 2017 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

// +build linux darwin

package overlay

import (
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"golang.org/x/net/context"
)

// Node is the node for both directories and files
type Node struct {
	fs *FS

	rpLock   sync.RWMutex
	realPath string

	isDir bool

	lock     sync.RWMutex
	flushers map[*Handle]bool
}

func (n *Node) getRealPath() string {
	n.rpLock.RLock()
	defer n.rpLock.RUnlock()
	return n.realPath
}

func (n *Node) updateRealPath(realPath string) {
	n.rpLock.Lock()
	defer n.rpLock.Unlock()
	n.realPath = realPath
}

var _ fs.NodeAccesser = (*Node)(nil)

// Access implements fs.NodeAccesser interface for *Node
func (n *Node) Access(ctx context.Context, a *fuse.AccessRequest) (err error) {
	time.Sleep(n.fs.latency)
	defer func() {
		log.Printf("%s.Access(%o): error=%v", n.getRealPath(), a.Mask, err)
	}()
	fi, err := os.Stat(n.getRealPath())
	if err != nil {
		return translateError(err)
	}
	if a.Mask&uint32(fi.Mode()>>6) != a.Mask {
		return fuse.EPERM
	}
	return nil
}

// Attr implements fs.Node interface for *Dir
func (n *Node) Attr(ctx context.Context, a *fuse.Attr) (err error) {
	time.Sleep(n.fs.latency)
	defer func() { log.Printf("%s.Attr(): %#+v error=%v", n.getRealPath(), a, err) }()
	fi, err := os.Stat(n.getRealPath())
	if err != nil {
		return translateError(err)
	}

	fillAttrWithFileInfo(a, fi)

	return nil
}

// Lookup implements fs.NodeRequestLookuper interface for *Node
func (n *Node) Lookup(ctx context.Context,
	name string) (ret fs.Node, err error) {
	time.Sleep(n.fs.latency)
	defer func() {
		log.Printf("%s.Lookup(%s): %#+v error=%v",
			n.getRealPath(), name, ret, err)
	}()

	if !n.isDir {
		return nil, fuse.ENOTSUP
	}

	p := filepath.Join(n.getRealPath(), name)
	fi, err := os.Stat(p)

	err = translateError(err)
	if err != nil {
		return nil, translateError(err)
	}

	var nn *Node
	if fi.IsDir() {
		nn = &Node{realPath: p, isDir: true, fs: n.fs}
	} else {
		nn = &Node{realPath: p, isDir: false, fs: n.fs}
	}

	n.fs.newNode(nn)
	return nn, nil
}

func getDirentsWithFileInfos(fis []os.FileInfo) (dirs []fuse.Dirent) {
	for _, fi := range fis {
		stat := fi.Sys().(*syscall.Stat_t)
		var tp fuse.DirentType

		switch {
		case fi.IsDir():
			tp = fuse.DT_Dir
		case fi.Mode().IsRegular():
			tp = fuse.DT_File
		default:
			panic("unsupported dirent type")
		}

		dirs = append(dirs, fuse.Dirent{
			Inode: stat.Ino,
			Name:  fi.Name(),
			Type:  tp,
		})
	}

	return dirs
}

func fuseOpenFlagsToOSFlagsAndPerms(
	f fuse.OpenFlags) (flag int, perm os.FileMode) {
	flag = int(f & fuse.OpenAccessModeMask)
	if f&fuse.OpenAppend != 0 {
		perm |= os.ModeAppend
	}
	if f&fuse.OpenCreate != 0 {
		flag |= os.O_CREATE
	}
	if f&fuse.OpenDirectory != 0 {
		perm |= os.ModeDir
	}
	if f&fuse.OpenExclusive != 0 {
		perm |= os.ModeExclusive
	}
	if f&fuse.OpenNonblock != 0 {
		log.Printf("fuse.OpenNonblock is set in OpenFlags but ignored")
	}
	if f&fuse.OpenSync != 0 {
		flag |= os.O_SYNC
	}
	if f&fuse.OpenTruncate != 0 {
		flag |= os.O_TRUNC
	}

	return flag, perm
}

func (n *Node) rememberHandle(h *Handle) {
	n.lock.Lock()
	defer n.lock.Unlock()
	if n.flushers == nil {
		n.flushers = make(map[*Handle]bool)
	}
	n.flushers[h] = true
}

func (n *Node) forgetHandle(h *Handle) {
	n.lock.Lock()
	defer n.lock.Unlock()
	if n.flushers == nil {
		return
	}
	delete(n.flushers, h)
}

var _ fs.NodeOpener = (*Node)(nil)

// Open implements fs.NodeOpener interface for *Node
func (n *Node) Open(ctx context.Context,
	req *fuse.OpenRequest, resp *fuse.OpenResponse) (h fs.Handle, err error) {
	time.Sleep(n.fs.latency)
	flags, perm := fuseOpenFlagsToOSFlagsAndPerms(req.Flags)
	defer func() {
		log.Printf("%s.Open(): %o %o error=%v",
			n.getRealPath(), flags, perm, err)
	}()

	opener := func() (*os.File, error) {
		return os.OpenFile(n.getRealPath(), flags, perm)
	}

	f, err := opener()
	if err != nil {
		return nil, translateError(err)
	}

	handle := &Handle{fs: n.fs, f: f, reopener: opener}
	n.rememberHandle(handle)
	handle.forgetter = func() {
		n.forgetHandle(handle)
	}
	return handle, nil
}

var _ fs.NodeCreater = (*Node)(nil)

// Create implements fs.NodeCreater interface for *Node
func (n *Node) Create(
	ctx context.Context, req *fuse.CreateRequest, resp *fuse.CreateResponse) (
	fsn fs.Node, fsh fs.Handle, err error) {
	time.Sleep(n.fs.latency)
	flags, _ := fuseOpenFlagsToOSFlagsAndPerms(req.Flags)
	name := filepath.Join(n.getRealPath(), req.Name)
	defer func() {
		log.Printf("%s.Create(%s): %o %o error=%v",
			n.getRealPath(), name, flags, req.Mode, err)
	}()

	opener := func() (f *os.File, err error) {
		return os.OpenFile(name, flags, req.Mode)
	}

	f, err := opener()
	if err != nil {
		return nil, nil, translateError(err)
	}

	h := &Handle{fs: n.fs, f: f, reopener: opener}

	node := &Node{
		realPath: filepath.Join(n.getRealPath(), req.Name),
		isDir:    req.Mode.IsDir(),
		fs:       n.fs,
	}
	node.rememberHandle(h)
	h.forgetter = func() {
		node.forgetHandle(h)
	}
	n.fs.newNode(node)
	return node, h, nil
}

var _ fs.NodeMkdirer = (*Node)(nil)

// Mkdir implements fs.NodeMkdirer interface for *Node
func (n *Node) Mkdir(ctx context.Context,
	req *fuse.MkdirRequest) (created fs.Node, err error) {
	time.Sleep(n.fs.latency)
	defer func() { log.Printf("%s.Mkdir(%s): error=%v", n.getRealPath(), req.Name, err) }()
	name := filepath.Join(n.getRealPath(), req.Name)
	if err = os.Mkdir(name, req.Mode); err != nil {
		return nil, translateError(err)
	}
	nn := &Node{realPath: name, isDir: true, fs: n.fs}
	n.fs.newNode(nn)
	return nn, nil
}

var _ fs.NodeRemover = (*Node)(nil)

// Remove implements fs.NodeRemover interface for *Node
func (n *Node) Remove(ctx context.Context, req *fuse.RemoveRequest) (err error) {
	time.Sleep(n.fs.latency)
	name := filepath.Join(n.getRealPath(), req.Name)
	defer func() { log.Printf("%s.Remove(%s): error=%v", n.getRealPath(), name, err) }()
	defer func() {
		if err == nil {
			n.fs.moveAllxattrs(ctx, name, "")
		}
	}()
	return os.Remove(name)
}

var _ fs.NodeFsyncer = (*Node)(nil)

// Fsync implements fs.NodeFsyncer interface for *Node
func (n *Node) Fsync(ctx context.Context, req *fuse.FsyncRequest) (err error) {
	time.Sleep(n.fs.latency)
	defer func() { log.Printf("%s.Fsync(): error=%v", n.getRealPath(), err) }()
	n.lock.RLock()
	defer n.lock.RUnlock()
	for h := range n.flushers {
		return h.f.Sync()
	}
	return fuse.EIO
}

var _ fs.NodeSetattrer = (*Node)(nil)

// Setattr implements fs.NodeSetattrer interface for *Node
func (n *Node) Setattr(ctx context.Context,
	req *fuse.SetattrRequest, resp *fuse.SetattrResponse) (err error) {
	time.Sleep(n.fs.latency)
	defer func() {
		log.Printf("%s.Setattr(valid=%x): error=%v", n.getRealPath(), req.Valid, err)
	}()
	if req.Valid.Size() {
		if err = syscall.Truncate(n.getRealPath(), int64(req.Size)); err != nil {
			return translateError(err)
		}
	}

	if req.Valid.Mtime() {
		var tvs [2]syscall.Timeval
		if !req.Valid.Atime() {
			tvs[0] = tToTv(time.Now())
		} else {
			tvs[0] = tToTv(req.Atime)
		}
		tvs[1] = tToTv(req.Mtime)
	}

	if req.Valid.Handle() {
		log.Printf("%s.Setattr(): unhandled request: req.Valid.Handle() == true",
			n.getRealPath())
	}

	if req.Valid.Mode() {
		if err = os.Chmod(n.getRealPath(), req.Mode); err != nil {
			return translateError(err)
		}
	}

	if req.Valid.Uid() || req.Valid.Gid() {
		if req.Valid.Uid() && req.Valid.Gid() {
			if err = os.Chown(n.getRealPath(), int(req.Uid), int(req.Gid)); err != nil {
				return translateError(err)
			}
		}
		fi, err := os.Stat(n.getRealPath())
		if err != nil {
			return translateError(err)
		}
		s := fi.Sys().(*syscall.Stat_t)
		if req.Valid.Uid() {
			if err = os.Chown(n.getRealPath(), int(req.Uid), int(s.Gid)); err != nil {
				return translateError(err)
			}
		} else {
			if err = os.Chown(n.getRealPath(), int(s.Uid), int(req.Gid)); err != nil {
				return translateError(err)
			}
		}
	}

	if err = n.setattrPlatformSpecific(ctx, req, resp); err != nil {
		return translateError(err)
	}

	fi, err := os.Stat(n.getRealPath())
	if err != nil {
		return translateError(err)
	}

	fillAttrWithFileInfo(&resp.Attr, fi)

	return nil
}

var _ fs.NodeRenamer = (*Node)(nil)

// Rename implements fs.NodeRenamer interface for *Node
func (n *Node) Rename(ctx context.Context,
	req *fuse.RenameRequest, newDir fs.Node) (err error) {
	time.Sleep(n.fs.latency)
	np := filepath.Join(newDir.(*Node).getRealPath(), req.NewName)
	op := filepath.Join(n.getRealPath(), req.OldName)
	defer func() {
		log.Printf("%s.Rename(%s->%s): error=%v",
			n.getRealPath(), op, np, err)
	}()
	defer func() {
		if err == nil {
			n.fs.moveAllxattrs(ctx, op, np)
			n.fs.nodeRenamed(op, np)
		}
	}()
	return os.Rename(op, np)
}

var _ fs.NodeGetxattrer = (*Node)(nil)

// Getxattr implements fs.Getxattrer interface for *Node
func (n *Node) Getxattr(ctx context.Context,
	req *fuse.GetxattrRequest, resp *fuse.GetxattrResponse) (err error) {
	time.Sleep(n.fs.latency)
	if !n.fs.inMemoryXattr {
		return fuse.ENOTSUP
	}

	defer func() {
		log.Printf("%s.Getxattr(%s): error=%v", n.getRealPath(), req.Name, err)
	}()
	n.fs.xlock.RLock()
	defer n.fs.xlock.RUnlock()
	if x := n.fs.xattrs[n.getRealPath()]; x != nil {

		var ok bool
		resp.Xattr, ok = x[req.Name]
		if ok {
			return nil
		}
	}
	return fuse.ENODATA
}

var _ fs.NodeListxattrer = (*Node)(nil)

// Listxattr implements fs.Listxattrer interface for *Node
func (n *Node) Listxattr(ctx context.Context,
	req *fuse.ListxattrRequest, resp *fuse.ListxattrResponse) (err error) {
	time.Sleep(n.fs.latency)
	if !n.fs.inMemoryXattr {
		return fuse.ENOTSUP
	}

	defer func() {
		log.Printf("%s.Listxattr(%d,%d): error=%v",
			n.getRealPath(), req.Position, req.Size, err)
	}()
	n.fs.xlock.RLock()
	defer n.fs.xlock.RUnlock()
	if x := n.fs.xattrs[n.getRealPath()]; x != nil {
		names := make([]string, 0)
		for k := range x {
			names = append(names, k)
		}
		sort.Strings(names)

		if int(req.Position) >= len(names) {
			return nil
		}
		names = names[int(req.Position):]

		s := int(req.Size)
		if s == 0 || s > len(names) {
			s = len(names)
		}
		if s > 0 {
			resp.Append(names[:s]...)
		}
	}

	return nil
}

var _ fs.NodeSetxattrer = (*Node)(nil)

// Setxattr implements fs.Setxattrer interface for *Node
func (n *Node) Setxattr(ctx context.Context,
	req *fuse.SetxattrRequest) (err error) {
	time.Sleep(n.fs.latency)
	if !n.fs.inMemoryXattr {
		return fuse.ENOTSUP
	}

	defer func() {
		log.Printf("%s.Setxattr(%s): error=%v", n.getRealPath(), req.Name, err)
	}()
	n.fs.xlock.Lock()
	defer n.fs.xlock.Unlock()
	if n.fs.xattrs[n.getRealPath()] == nil {
		n.fs.xattrs[n.getRealPath()] = make(map[string][]byte)
	}
	buf := make([]byte, len(req.Xattr))
	copy(buf, req.Xattr)

	n.fs.xattrs[n.getRealPath()][req.Name] = buf
	return nil
}

var _ fs.NodeRemovexattrer = (*Node)(nil)

// Removexattr implements fs.Removexattrer interface for *Node
func (n *Node) Removexattr(ctx context.Context,
	req *fuse.RemovexattrRequest) (err error) {
	time.Sleep(n.fs.latency)
	if !n.fs.inMemoryXattr {
		return fuse.ENOTSUP
	}

	defer func() {
		log.Printf("%s.Removexattr(%s): error=%v", n.getRealPath(), req.Name, err)
	}()
	n.fs.xlock.Lock()
	defer n.fs.xlock.Unlock()

	name := req.Name

	if x := n.fs.xattrs[n.getRealPath()]; x != nil {
		var ok bool
		_, ok = x[name]
		if ok {
			delete(x, name)
			return nil
		}
	}
	return fuse.ENODATA
}

var _ fs.NodeForgetter = (*Node)(nil)

// Forget implements fs.NodeForgetter interface for *Node
func (n *Node) Forget() {
	n.fs.forgetNode(n)
}
