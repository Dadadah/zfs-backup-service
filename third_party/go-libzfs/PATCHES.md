# Local patches to go-libzfs v0.4.0

This directory is a copy of
[github.com/bicomsystems/go-libzfs](https://github.com/bicomsystems/go-libzfs)
at tag **v0.4.0** (BSD-3-Clause license, see `LICENSE.md`), with a few small local
patches. It is referenced from the root `go.mod` via a `replace` directive,
so `import "github.com/bicomsystems/go-libzfs"` resolves to this copy.

The upstream project is unmaintained and v0.4.0 does not compile unmodified
against modern OpenZFS (2.2/2.3) with recent GCC. The patches below are the
minimal changes needed to build this service against current OpenZFS.

## 1. `zpool.c` — stub out `go_zpool_search_import()`

The legacy `zpool_search_import(libzfs_handle_t *, importargs_t *,
libzfs_config_ops *)` API was removed from OpenZFS 2.x libzfs, so the
original implementation no longer compiles. Pool *import* discovery is not
used by this service; the function now returns `NULL` (i.e. "no importable
pools found"). The Go wrappers `PoolImport*` / `PoolImportSearch` still
compile and simply report that nothing was found.

## 2. `zfs.c` / `zpool.c` — const correctness for `nvlist_lookup_string()`

OpenZFS 2.x declares `nvlist_lookup_string()` with a `const char **`
result argument. GCC 14+ promotes the resulting incompatible-pointer-type
warning to a hard error. The affected local variables (in
`read_user_property()` in zfs.c and in `get_vdev_type()` /
`get_vdev_path()` / `get_zpool_name()` / `get_zpool_comment()` in zpool.c)
are declared `const char *` instead of `char *`.

## 3. `zpool.go` — `pool_scan_stat` field removal

The `pss_to_process` field of `pool_scan_stat` was removed in OpenZFS 2.x.
The vdev scan-stats reader no longer reads it (`ScanStat.ToProcess` stays
zero). Scan statistics are not used by this service.

## 4. New file `receive_into.go` — `ReceiveInto()`

The stock `(*Dataset).Receive()` passes the *opened* dataset's path to
`zfs_receive(3)`, so the destination must already be openable. When
receiving a backup stream the destination dataset normally does not exist
yet (the stream creates it). `ReceiveInto(dst, inf, flags)` calls
`zfs_receive(3)` directly with an explicit destination path.

## 5. New file `fsacl.go` — delegated permission queries

Adds two small helpers built on the public `zfs_get_fsacl(3)` API (which
any local user may call):

* `Dataset.DelegatedPermissionMask(uid, gids, perms)` — bit i of the
  result is set when the dataset's delegated ACL ("zfs allow") effectively
  grants `perms[i]` to the user/group ids, mirroring the kernel's own
  access check (`dsl_deleg_access_impl`): the dataset's own entry matches
  both its local and descend whokeys, ancestor entries only their descend
  whokeys. Used by the service startup self-check to fail fast with an
  actionable message when the required delegated privileges are missing
  (running as a non-root user or in a container).
* `LibzfsInitFailed()` — true when `libzfs_init()` could not create its
  handle at process start (e.g. `/dev/zfs` missing or not accessible, as
  in a container where the device was not passed in).

The C preamble in `fsacl.go` documents the exact `zfs_get_fsacl` nvlist
layout (source dataset → whokey → permissions, built in
`module/zfs/dsl_deleg.c`) and the whokey format (`ul$<uid>`, `ud$<uid>`,
`gl$<gid>`, `gd$<gid>`, `el$`, `ed$` — per `zfs_deleg_whokey` /
`zfs_validate_who`). Two API pitfalls are handled deliberately:

* Only the **legacy** lookup functions (`nvlist_lookup_nvlist`,
  `nvlist_lookup_boolean`) are used. The `fnvlist_*` lookup variants are
  `VERIFY0` wrappers in OpenZFS 2.2–2.4 that **abort the process** when
  the entry is absent — and absent whokeys/permissions are the normal
  case here.
* Permission entries in the fsa are **valueless booleans** (the kernel
  adds them with `fnvlist_add_boolean`), which is what
  `nvlist_lookup_boolean` matches (not `nvlist_lookup_boolean_value`).

Everything else is unmodified.
