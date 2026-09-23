// Copyright 2026 dadadah
// SPDX-License-Identifier: MIT
//
// zfs-backup-service addition to the vendored go-libzfs copy (see
// PATCHES.md). Exposes the delegated permission table ("zfs allow") of a
// dataset so callers can check, without shelling out to the zfs CLI, which
// of a set of named permissions are effective on a dataset for a given
// user/group id. Also reports whether libzfs could be initialized at all
// (i.e. /dev/zfs is present and accessible).
package zfs

/*
#include <stdio.h>
#include <libzfs.h>
#include <libnvpair.h>
#include "common.h"

// The nvlist returned by zfs_get_fsacl() (ZFS_IOC_GET_FSACL) has this
// layout in OpenZFS 2.2 through 2.4 (built in module/zfs/dsl_deleg.c):
//
//   {
//     "<source dataset>": {              // target, then its ancestors
//       "ul$<uid>": { "receive": <boolean>, ... },  // whokey -> perms
//       "ud$<uid>": { ... },
//       "gl$<gid>": { ... },
//       "gd$<gid>": { ... },
//       "el$":       { ... },            // everyone local
//       "ed$":       { ... },            // everyone descend
//       ... (create-set/named-set entries: Ul$..., s-$name — ignored)
//     },
//   }
//
// The permission entries are VALUELESS booleans (the kernel adds them with
// fnvlist_add_boolean), so nvlist_lookup_boolean() is the matching lookup.
// All lookups below use the legacy (non-VERIFY) APIs on purpose: the
// fnvlist_* lookup variants are VERIFY0 wrappers in OpenZFS 2.2/2.3/2.4
// that ABORT the process when the entry is absent — and absent whokeys and
// permissions are the normal case here.
//
// Whokey format is per zfs_deleg_whokey()/zfs_validate_who():
// type char, inheritance char (l/d), '$', then the id — e.g. "ul$1000",
// "gd$3000"; everyone keys have no id ("el$", "ed$").
//
// Grant semantics mirror the kernel's dsl_deleg_access_impl(): the target
// dataset's own entry matches both its local ("l") and descend ("d")
// whokeys; entries for ancestor datasets match only descend whokeys.
// (Create-set entries define what newly created children get; they do not
// grant anything on the dataset itself, so they are not considered.)

// OR into *mask the bits of `perms` granted by the whokey entry.
static void
zbs_or_grants(uint64_t *mask, nvlist_t *whos, const char *whokey,
    const char **perms, int nperms)
{
	for (int p = 0; p < nperms; p++) {
		nvlist_t *plist;

		if (nvlist_lookup_nvlist(whos, whokey, &plist) != 0)
			return; // whokey absent: grants nothing
		if (nvlist_lookup_boolean(plist, perms[p]) == 0)
			*mask |= (uint64_t)1 << p;
	}
}

// Bitmask over `perms`: bit i is set when the delegated ACL effectively
// grants perms[i] to user `uid` (member of the groups in `gids`) or
// "everyone" on the dataset named `self` — local+descend grants from
// `self`'s own entry, descend grants from each ancestor entry.
static uint64_t
zbs_delegated_perm_mask(nvlist_t *acl, const char *self, uint64_t uid,
    const uint64_t *gids, int ngids, const char **perms, int nperms)
{
	uint64_t mask = 0;
	nvpair_t *srcp;
	char key[64];

	for (srcp = NULL; (srcp = nvlist_next_nvpair(acl, srcp)) != NULL;) {
		nvlist_t *whos;
		boolean_t own;

		if (nvpair_value_nvlist(srcp, &whos) != 0)
			continue;
		own = (self != NULL && strcmp(nvpair_name(srcp), self) == 0);

		// user: local only from the dataset's own entry
		if (own) {
			(void) snprintf(key, sizeof (key), "ul$%llu",
			    (unsigned long long)uid);
			zbs_or_grants(&mask, whos, key, perms, nperms);
		}
		(void) snprintf(key, sizeof (key), "ud$%llu",
		    (unsigned long long)uid);
		zbs_or_grants(&mask, whos, key, perms, nperms);

		for (int g = 0; g < ngids; g++) {
			if (own) {
				(void) snprintf(key, sizeof (key), "gl$%llu",
				    (unsigned long long)gids[g]);
				zbs_or_grants(&mask, whos, key, perms, nperms);
			}
			(void) snprintf(key, sizeof (key), "gd$%llu",
			    (unsigned long long)gids[g]);
			zbs_or_grants(&mask, whos, key, perms, nperms);
		}

		if (own)
			zbs_or_grants(&mask, whos, "el$", perms, nperms);
		zbs_or_grants(&mask, whos, "ed$", perms, nperms);
	}

	return (mask);
}

static int
zbs_libzfs_init_failed(void)
{
	return (libzfsHandle == NULL);
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// LibzfsInitFailed reports whether the underlying libzfs handle could not
// be created at process start — most commonly because /dev/zfs is missing
// or not accessible (e.g. the device was not passed into a container).
// When it returns true, no ZFS operation can succeed.
func LibzfsInitFailed() bool {
	return C.zbs_libzfs_init_failed() != 0
}

// DelegatedPermissionMask reports which of the named delegated permissions
// (the names accepted by "zfs allow": snapshot, send, create, mount,
// receive, ...) are effectively granted on this dataset for user id `uid`
// and the groups in `gids`. Bit i of the returned mask is set when
// perms[i] is granted — local and descend grants from the dataset's own
// "zfs allow" entries, plus descend grants inherited from its ancestors,
// which is exactly the set of grants the kernel enforces.
func (d *Dataset) DelegatedPermissionMask(uid uint64, gids []uint64, perms []string) (uint64, error) {
	var nvl *C.nvlist_t

	name, err := d.Path()
	if err != nil {
		return 0, err
	}
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	if 0 != C.zfs_get_fsacl(d.list.zh, &nvl) {
		return 0, fmt.Errorf("zfs_get_fsacl on %s: %s", name, LastError())
	}
	defer C.nvlist_free(nvl)

	var cPerms []*C.char
	for _, p := range perms {
		cPerms = append(cPerms, C.CString(p))
	}
	defer func() {
		for _, cp := range cPerms {
			C.free(unsafe.Pointer(cp))
		}
	}()

	var cGids []C.uint64_t
	for _, g := range gids {
		cGids = append(cGids, C.uint64_t(g))
	}

	var mask C.uint64_t
	if len(cPerms) > 0 {
		var cgids *C.uint64_t
		if len(cGids) > 0 {
			cgids = &cGids[0]
		}
		mask = C.zbs_delegated_perm_mask(nvl, cName, C.uint64_t(uid),
			cgids, C.int(len(cGids)), &cPerms[0], C.int(len(cPerms)))
	}
	return uint64(mask), nil
}
