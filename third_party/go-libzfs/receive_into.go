// Package zfs - addition by zfs-backup-service (see PATCHES.md).
//
// (d *Dataset).Receive() can only receive into a dataset that is already
// open (it passes the opened dataset's path to zfs_receive(3)), but the
// destination of a received stream normally does not exist before the
// receive. This file adds ReceiveInto() which calls zfs_receive(3) with an
// explicit destination path, so the final dataset may be created by the
// receive itself (its parent must already exist).
package zfs

/*
#include <stdlib.h>
#include <libzfs.h>
#include "common.h"
*/
import "C"

import (
	"fmt"
	"os"
	"unsafe"
)

// ReceiveInto receives the snapshot stream read from inf into the dataset
// named by dst (e.g. "pool/backups/tank/data"). dst does not need to exist
// beforehand. With the default flags the last path component of dst must
// match the (top) dataset name in the stream; with RecvFlags.IsPrefix (-d)
// dst is a prefix and the stream's dataset path — without its first element
// (the pool name) — is created underneath it.
func ReceiveInto(dst string, inf *os.File, flags RecvFlags) error {
	cflags := to_recvflags_t(&flags)
	defer C.free(unsafe.Pointer(cflags))

	cdst := C.CString(dst)
	defer C.free(unsafe.Pointer(cdst))

	if ec := C.zfs_receive(C.libzfsHandle, cdst, nil, cflags, C.int(inf.Fd()), nil); ec != 0 {
		return fmt.Errorf("zfs receive into %s failed: %s", dst, LastError().Error())
	}
	return nil
}
