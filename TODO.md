Storage aspects
- etag propagation
  - store in extended attribute
- size accounting
  - store in extended attribute
- trash
  - store using freedesktop spec?
  - expose via rest / grpc 
- versions
  - store using .versions tree owned by root?
  - expose via rest / grpc
- share persistence
  - store using acls
  - should be transparent
- quota

# TODO
- [x] pass xattr calls to underlying fs
- [ ] generate fileid and store in trusted.ocis.fileid attribute?
- [ ] calculate etag and store in trusted.ocis.etag
- [ ] propagate etag up to root node or node with trusted.ocis.root=true (check eos: sys.mtime.propagation=1               : if present a change under this directory propagates an mtime change up to all parents until the attribute is not present anymore)
- shouldd we prepagate etag or mtime? only calculate etag? what about touch?

# ETAG
eos uses inode:mtime or inode:checksum for the etag: https://gitlab.cern.ch/dss/eos/-/blob/master/namespace/utils/Etag.cc#L52-59

but etags should not be inode based, otherwise, restoring a backup will change the etag. See http://joshua.schachter.org/2006/11/apache-etags

we should use our generated fileid + mtime + sizeP

# GlusterFS
- might be an interesting candidate for the storage because it uses extended attributes for the [gfid to path lookup](https://docs.gluster.org/en/latest/Troubleshooting/gfid-to-path/) and a [lot of other things](http://oliviercontant.com/gluster-glusterfs-extended-attribute/). Also see the [Architecture](https://docs.gluster.org/en/latest/Quick-Start-Guide/Architecture/)