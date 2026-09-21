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

## 4. `zpool.go` — `pool_scan_stat` field removal

The `pss_to_process` field of `pool_scan_stat` was removed in OpenZFS 2.x.
The vdev scan-stats reader no longer reads it (`ScanStat.ToProcess` stays
zero). Scan statistics are not used by this service.

## 5. New file `receive_into.go` — `ReceiveInto()`

The stock `(*Dataset).Receive()` passes the *opened* dataset's path to
`zfs_receive(3)`, so the destination must already be openable. When
receiving a backup stream the destination dataset normally does not exist
yet (the stream creates it). `ReceiveInto(dst, inf, flags)` calls
`zfs_receive(3)` directly with an explicit destination path.

Everything else is unmodified.
