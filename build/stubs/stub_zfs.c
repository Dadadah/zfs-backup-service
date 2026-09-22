/*
 * Copyright 2026 dadadah
 *
 * SPDX-License-Identifier: MIT
 *
 * Stub implementations of the OpenZFS userspace API symbols used by the
 * vendored go-libzfs package.
 *
 * This file is ONLY used by the `make stub-build` mode (see prepare.sh),
 * which lets `go build` compile the full cgo binary on machines that have
 * no ZFS installation. The stub is a *smart* fake: dataset handles are
 * accepted (they remember their name, and "pool/ds@snap" is reported as a
 * snapshot), create/snapshot succeed, and zfs_send / zfs_receive exchange
 * a small marker stream. That is enough to exercise the whole send ->
 * HTTP pipe -> receive pipeline end-to-end without real ZFS. Operations
 * outside that pipeline fail with a clear "stub" error.
 *
 * On a real ZFS node, build without these stubs (plain `make`).
 *
 * Signatures below mirror OpenZFS 2.3.x include/libzfs.h exactly; keep
 * them in sync when changing the pinned OpenZFS version in prepare.sh.
 */
#include <errno.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#include <libzfs.h>
#include <zfs_prop.h>

static char last_err[256] = "stub: no error set";
static void *stub_handle_storage;

static void
set_err(const char *msg)
{
	snprintf(last_err, sizeof(last_err), "%s", msg);
}

#define STUB_ERR "stub: no ZFS available (build with real libzfs)"

/* ------------------------------------------------------------------ */
/* libzfs handle                                                       */
/* ------------------------------------------------------------------ */

libzfs_handle_t *
libzfs_init(void)
{
	if (!stub_handle_storage)
		stub_handle_storage = malloc(16);
	return (libzfs_handle_t *)stub_handle_storage;
}

void
libzfs_close(libzfs_handle_t *hdl)
{
	if (hdl == (libzfs_handle_t *)stub_handle_storage) {
		free(stub_handle_storage);
		stub_handle_storage = NULL;
	}
}

int
libzfs_errno(libzfs_handle_t *hdl)
{
	(void)hdl;
	return -1;
}

const char *
libzfs_error_description(libzfs_handle_t *hdl)
{
	(void)hdl;
	return last_err;
}

const char *
libzfs_error_action(libzfs_handle_t *hdl)
{
	(void)hdl;
	return "";
}

int
zfs_standard_error(libzfs_handle_t *hdl, int error, const char *fmt)
{
	(void)hdl;
	(void)error;
	(void)fmt;
	return 0;
}

/* ------------------------------------------------------------------ */
/* libspl                                                              */
/* ------------------------------------------------------------------ */

void
libspl_assertf(const char *file, const char *func, int line, const char *fmt,
    ...)
{
	fprintf(stderr, "stub assertion failed at %s:%d (%s): %s\n",
	    file, line, func, fmt);
	abort();
}

/* ------------------------------------------------------------------ */
/* nvlist / nvpair                                                     */
/* ------------------------------------------------------------------ */

int
nvlist_alloc(nvlist_t **nvl, uint_t flags, int type)
{
	(void)flags;
	(void)type;
	*nvl = calloc(1, 64);
	return *nvl == NULL ? -1 : 0;
}

void
nvlist_free(nvlist_t *nvl)
{
	if (nvl != NULL)
		free(nvl);
}

int
nvlist_add_string(nvlist_t *nvl, const char *name, const char *val)
{
	(void)nvl;
	(void)name;
	(void)val;
	return 0;
}

int
nvlist_add_uint32(nvlist_t *nvl, const char *name, uint32_t val)
{
	(void)nvl;
	(void)name;
	(void)val;
	return 0;
}

int
nvlist_add_uint64(nvlist_t *nvl, const char *name, uint64_t val)
{
	(void)nvl;
	(void)name;
	(void)val;
	return 0;
}

void
fnvlist_add_boolean(nvlist_t *nvl, const char *name)
{
	(void)nvl;
	(void)name;
}

int
nvlist_add_nvlist_array(nvlist_t *nvl, const char *name,
    const nvlist_t * const *arr, uint_t arrlen)
{
	(void)nvl;
	(void)name;
	(void)arr;
	(void)arrlen;
	return 0;
}

int
nvlist_lookup_string(const nvlist_t *nvl, const char *name,
    const char **val)
{
	(void)nvl;
	(void)name;
	(void)val;
	return -1;
}

int
nvlist_lookup_uint64(const nvlist_t *nvl, const char *name,
    uint64_t *val)
{
	(void)nvl;
	(void)name;
	(void)val;
	return -1;
}

int
nvlist_lookup_nvlist(nvlist_t *nvl, const char *name,
    nvlist_t **val)
{
	(void)nvl;
	(void)name;
	(void)val;
	return -1;
}

int
nvlist_lookup_nvlist_array(nvlist_t *nvl, const char *name,
    nvlist_t ***arr, uint_t *arrlen)
{
	(void)nvl;
	(void)name;
	(void)arr;
	(void)arrlen;
	return -1;
}

int
nvlist_lookup_uint64_array(nvlist_t *nvl, const char *name,
    uint64_t **arr, uint_t *arrlen)
{
	(void)nvl;
	(void)name;
	(void)arr;
	(void)arrlen;
	return -1;
}

boolean_t
nvlist_exists(const nvlist_t *nvl, const char *name)
{
	(void)nvl;
	(void)name;
	return 0;
}

nvpair_t *
nvlist_next_nvpair(nvlist_t *nvl, const nvpair_t *prev)
{
	(void)nvl;
	(void)prev;
	return NULL;
}

const char *
nvpair_name(const nvpair_t *nvp)
{
	(void)nvp;
	return "stub";
}

int
nvpair_value_uint64(const nvpair_t *nvp, uint64_t *val)
{
	(void)nvp;
	*val = 0;
	return 0;
}

int
nvpair_value_nvlist(nvpair_t *nvp, nvlist_t **val)
{
	(void)nvp;
	if (val != NULL)
		*val = NULL;
	return -1;
}

/* ------------------------------------------------------------------ */
/* zfs dataset                                                         */
/*                                                                     */
/* The stub is a *smart* fake: handles remember their name and send/   */
/* receive exchange a small marker stream, so the whole pipeline       */
/* (dataset open -> snapshot -> zfs send -> HTTP pipe -> zfs receive)  */
/* can be exercised end-to-end on a machine without ZFS.               */
/* ------------------------------------------------------------------ */

#define STUB_STREAM_MAGIC "STUBZFSSEND\n"

struct stub_zfs_handle {
	char name[256];
};

zfs_handle_t *
zfs_open(libzfs_handle_t *hdl, const char *name, int flags)
{
	struct stub_zfs_handle *sh;
	const char *missing;

	(void)hdl;
	(void)flags;
	/* Test hook: ZFS_STUB_MISSING is a name that zfs_open must report
	 * as absent, so a test can drive the create-a-snapshot path. */
	missing = getenv("ZFS_STUB_MISSING");
	if (missing != NULL && *missing != '\0' &&
	    strcmp(name, missing) == 0) {
		set_err("stub: dataset not found");
		return NULL;
	}
	sh = calloc(1, sizeof(*sh));
	if (sh == NULL) {
		set_err(STUB_ERR);
		return NULL;
	}
	snprintf(sh->name, sizeof(sh->name), "%s", name);
	return (zfs_handle_t *)sh;
}

void
zfs_close(zfs_handle_t *zh)
{
	free(zh);
}

const char *
zfs_get_name(const zfs_handle_t *zh)
{
	return ((const struct stub_zfs_handle *)zh)->name;
}

zfs_type_t
zfs_get_type(const zfs_handle_t *zh)
{
	const char *name = zfs_get_name(zh);
	/* Anything named "pool/ds@snap" is treated as a snapshot, which is
	 * what the service's Send()/SendFrom() paths require. */
	if (name != NULL && strchr(name, '@') != NULL) {
		return ZFS_TYPE_SNAPSHOT;
	}
	return ZFS_TYPE_FILESYSTEM;
}

zpool_handle_t *
zfs_get_pool_handle(const zfs_handle_t *zh)
{
	(void)zh;
	return NULL;
}

int
zfs_create(libzfs_handle_t *hdl, const char *name, zfs_type_t type,
    nvlist_t *nv)
{
	(void)hdl;
	(void)name;
	(void)type;
	(void)nv;
	return 0;
}

int
zfs_destroy(zfs_handle_t *zh, boolean_t defer)
{
	(void)zh;
	(void)defer;
	return 0;
}

int
zfs_snapshot(libzfs_handle_t *hdl, const char *name, boolean_t recurse,
    nvlist_t *nv)
{
	(void)hdl;
	(void)nv;
	/* Record the snapshot + recursion flag so a test can verify a
	 * recursive transfer created a recursive snapshot. */
	{
		FILE *lf = fopen("stub-transfer.log", "a");
		if (lf != NULL) {
			fprintf(lf, "SNAP name=%s recurse=%d\n", name,
			    (int)recurse);
			fclose(lf);
		}
	}
	return 0;
}

int
zfs_clone(zfs_handle_t *zh, const char *name, nvlist_t *nv)
{
	(void)zh;
	(void)name;
	(void)nv;
	set_err(STUB_ERR);
	return -1;
}

int
zfs_rollback(zfs_handle_t *zh, zfs_handle_t *snap, boolean_t force)
{
	(void)zh;
	(void)snap;
	(void)force;
	set_err(STUB_ERR);
	return -1;
}

int
zfs_promote(zfs_handle_t *zh)
{
	(void)zh;
	set_err(STUB_ERR);
	return -1;
}

int
zfs_rename(zfs_handle_t *zh, const char *newname, renameflags_t flags)
{
	(void)zh;
	(void)newname;
	(void)flags;
	set_err(STUB_ERR);
	return -1;
}

boolean_t
zfs_is_mounted(zfs_handle_t *zh, char **where)
{
	(void)zh;
	if (where != NULL)
		*where = NULL;
	return 0;
}

int
zfs_mount(zfs_handle_t *zh, const char *options, int flags)
{
	(void)zh;
	(void)options;
	(void)flags;
	set_err(STUB_ERR);
	return -1;
}

int
zfs_unmount(zfs_handle_t *zh, const char *tag, int flags)
{
	(void)zh;
	(void)tag;
	(void)flags;
	set_err(STUB_ERR);
	return -1;
}

int
zfs_unmountall(zfs_handle_t *zh, int flags)
{
	(void)zh;
	(void)flags;
	set_err(STUB_ERR);
	return -1;
}

int
zfs_prop_get(zfs_handle_t *zh, zfs_prop_t prop, char *buffer, size_t buflen,
    zprop_source_t *source, char *statbuf, size_t statlen, boolean_t parse)
{
	(void)zh;
	(void)prop;
	(void)buffer;
	(void)buflen;
	(void)source;
	(void)statbuf;
	(void)statlen;
	(void)parse;
	return -1;
}

int
zfs_prop_set(zfs_handle_t *zh, const char *prop, const char *value)
{
	(void)zh;
	(void)prop;
	(void)value;
	set_err(STUB_ERR);
	return -1;
}

const char *
zfs_prop_to_name(zfs_prop_t prop)
{
	(void)prop;
	return "stub";
}

zfs_prop_t
zfs_name_to_prop(const char *name)
{
	(void)name;
	return ZPROP_INVAL;
}

void
zfs_refresh_properties(zfs_handle_t *zh)
{
	(void)zh;
}

nvlist_t *
zfs_get_user_props(zfs_handle_t *zh)
{
	(void)zh;
	return NULL;
}

int
zfs_iter_root(libzfs_handle_t *hdl, zfs_iter_f f, void *data)
{
	(void)hdl;
	(void)f;
	(void)data;
	return -1;
}

int
zfs_iter_children(zfs_handle_t *zh, zfs_iter_f f, void *data)
{
	(void)zh;
	(void)f;
	(void)data;
	return -1;
}

int
zfs_ioctl(libzfs_handle_t *hdl, int cmd, struct zfs_cmd *data)
{
	(void)hdl;
	(void)cmd;
	(void)data;
	set_err(STUB_ERR);
	return -1;
}

int
zfs_hold(zfs_handle_t *zh, const char *name, const char *tag,
    boolean_t heal, int timestamp)
{
	(void)zh;
	(void)name;
	(void)tag;
	(void)heal;
	(void)timestamp;
	set_err(STUB_ERR);
	return -1;
}

int
zfs_release(zfs_handle_t *zh, const char *name, const char *tag,
    boolean_t heal)
{
	(void)zh;
	(void)name;
	(void)tag;
	(void)heal;
	set_err(STUB_ERR);
	return -1;
}

int
zfs_get_holds(zfs_handle_t *zh, nvlist_t **tagtstamps)
{
	(void)zh;
	(void)tagtstamps;
	set_err(STUB_ERR);
	return -1;
}

static ssize_t
write_full(int fd, const void *p, size_t len)
{
	size_t off = 0;
	while (off < len) {
		ssize_t w = write(fd, (const char *)p + off, len - off);
		if (w < 0) {
			if (errno == EINTR)
				continue;
			return -1;
		}
		off += (size_t)w;
	}
	return (ssize_t)len;
}

int
zfs_send(zfs_handle_t *zh, const char *fromsnap, const char *tosnap,
    sendflags_t *sf, int fd, snapfilter_cb_t cb, void *data,
    nvlist_t **holds)
{
	char buf[512];
	char fbuf[160];
	int n;
	int fn = 0;

	(void)cb;
	(void)data;
	(void)holds;
	n = snprintf(buf, sizeof(buf), "%s%s -> %s (from=%s)\n",
	    STUB_STREAM_MAGIC, zfs_get_name(zh),
	    tosnap != NULL ? tosnap : "?",
	    fromsnap != NULL ? fromsnap : "none");
	if (n <= 0) {
		set_err(STUB_ERR);
		return -1;
	}
	if (write_full(fd, buf, (size_t)n) != n) {
		set_err(STUB_ERR);
		return -1;
	}
	/* Echo the effective send flags into the stream so a test can
	 * verify they crossed the cgo boundary (see the matching RECV line
	 * in zfs_receive / stub-transfer.log). */
	if (sf != NULL) {
		fn = snprintf(fbuf, sizeof(fbuf),
		    "#sendflags raw=%d replicate=%d compress=%d embed=%d large=%d\n",
		    (int)sf->raw, (int)sf->replicate, (int)sf->compress,
		    (int)sf->embed_data, (int)sf->largeblock);
		if (fn > 0 && write_full(fd, fbuf, (size_t)fn) != fn) {
			set_err(STUB_ERR);
			return -1;
		}
	}
	/* Optionally pad the stream to ZFS_STUB_STREAM_BYTES so a test can
	 * push more than the 64KB pipe buffer and exercise blocking writes. */
	const char *env = getenv("ZFS_STUB_STREAM_BYTES");
	if (env != NULL) {
		long total = atol(env);
		long sent = n + fn;
		char pad[65536];
		memset(pad, 'x', sizeof(pad));
		while (sent < total) {
			size_t chunk = (size_t)((total - sent) < (long)sizeof(pad) ?
			    (total - sent) : (long)sizeof(pad));
			if (write_full(fd, pad, chunk) < 0) {
				set_err(STUB_ERR);
				return -1;
			}
			sent += (long)chunk;
		}
	}
	return 0;
}

int
zfs_send_resume(libzfs_handle_t *hdl, sendflags_t *sf, int outfd,
    const char *resume_token)
{
	(void)hdl;
	(void)sf;
	(void)outfd;
	(void)resume_token;
	set_err(STUB_ERR);
	return -1;
}

nvlist_t *
zfs_send_resume_token_to_nvlist(libzfs_handle_t *hdl, const char *token)
{
	(void)hdl;
	(void)token;
	set_err(STUB_ERR);
	return NULL;
}

int
zfs_receive(libzfs_handle_t *hdl, const char *dst, nvlist_t *props,
    recvflags_t *rf, int fd, avl_tree_t *skips)
{
	char buf[4096];
	char head[4096] = {0};
	size_t got = 0, total = 0;
	ssize_t r;

	(void)hdl;
	(void)props;
	(void)skips;
	while ((r = read(fd, buf, sizeof(buf))) > 0) {
		if (got < sizeof(head)) {
			size_t take = (size_t)r < sizeof(head) - got ?
			    (size_t)r : sizeof(head) - got;
			memcpy(head + got, buf, take);
			got += take;
		}
		total += (size_t)r;
	}
	if (r < 0) {
		set_err(STUB_ERR);
		return -1;
	}
	if (memcmp(head, STUB_STREAM_MAGIC, sizeof(STUB_STREAM_MAGIC) - 1) !=
	    0 || total < sizeof(STUB_STREAM_MAGIC)) {
		set_err("stub: not a stub zfs send stream");
		return -1;
	}
	/* Record the effective receive flags + the stream's #sendflags line
	 * in stub-transfer.log (cwd) so a test can verify the flags crossed
	 * the cgo boundary on both sides of the transfer. */
	{
		FILE *lf = fopen("stub-transfer.log", "a");
		if (lf != NULL) {
			const char *sfl = "#sendflags (none)";
			size_t i;
			for (i = 0; i + 10 <= got; i++) {
				if (memcmp(head + i, "#sendflags", 10) == 0) {
					sfl = head + i;
					break;
				}
			}
			fprintf(lf,
			    "RECV dst=%s force=%d nomount=%d verbose=%d resumable=%d prefix=%d | %.*s\n",
			    dst, rf != NULL ? (int)rf->force : -1,
			    rf != NULL ? (int)rf->nomount : -1,
			    rf != NULL ? (int)rf->verbose : -1,
			    rf != NULL ? (int)rf->resumable : -1,
			    rf != NULL ? (int)rf->isprefix : -1,
			    (int)strcspn(sfl, "\n"), sfl);
			fclose(lf);
		}
	}
	return 0;
}

/* ------------------------------------------------------------------ */
/* zpool                                                               */
/* ------------------------------------------------------------------ */

zpool_handle_t *
zpool_open(libzfs_handle_t *hdl, const char *name)
{
	(void)hdl;
	(void)name;
	set_err(STUB_ERR);
	return NULL;
}

void
zpool_close(zpool_handle_t *zph)
{
	(void)zph;
}

const char *
zpool_get_name(zpool_handle_t *zph)
{
	(void)zph;
	return "stub";
}

int
zpool_get_prop(zpool_handle_t *zph, zpool_prop_t prop, char *cval,
    size_t cval_size, zprop_source_t *source, boolean_t numeric)
{
	(void)zph;
	(void)prop;
	(void)cval;
	(void)cval_size;
	(void)source;
	(void)numeric;
	return -1;
}

int
zpool_set_prop(zpool_handle_t *zph, const char *name, const char *value)
{
	(void)zph;
	(void)name;
	(void)value;
	set_err(STUB_ERR);
	return -1;
}

int
zpool_get_state(zpool_handle_t *zph)
{
	(void)zph;
	return 0;
}

zpool_status_t
zpool_get_status(zpool_handle_t *zph, const char **status,
    zpool_errata_t *errata)
{
	(void)zph;
	if (status != NULL)
		*status = "stub";
	if (errata != NULL)
		*errata = ZPOOL_ERRATA_NONE;
	return ZPOOL_STATUS_OK;
}

int
zpool_iter(libzfs_handle_t *hdl, zpool_iter_f f, void *data)
{
	(void)hdl;
	(void)f;
	(void)data;
	return -1;
}

zpool_handle_t *
zpool_next(libzfs_handle_t *hdl, zpool_handle_t *zph)
{
	(void)hdl;
	(void)zph;
	return NULL;
}

const char *
zpool_prop_to_name(zpool_prop_t prop)
{
	(void)prop;
	return "stub";
}

zpool_prop_t
zpool_name_to_prop(const char *name)
{
	(void)name;
	return ZPOOL_PROP_INVAL;
}

boolean_t
zpool_prop_feature(const char *name)
{
	(void)name;
	return 0;
}

int
zpool_prop_get_feature(zpool_handle_t *zph, const char *name, char *cval,
    size_t cval_size)
{
	(void)zph;
	(void)name;
	(void)cval;
	(void)cval_size;
	return -1;
}

const char *
zpool_pool_state_to_name(pool_state_t state)
{
	(void)state;
	return "stub";
}

nvlist_t *
zpool_get_config(zpool_handle_t *zph, nvlist_t **nv)
{
	(void)zph;
	if (nv != NULL)
		*nv = NULL;
	return NULL;
}

int
zpool_create(libzfs_handle_t *hdl, const char *name, nvlist_t *nv,
    nvlist_t *ppnv, nvlist_t *fvnv)
{
	(void)hdl;
	(void)name;
	(void)nv;
	(void)ppnv;
	(void)fvnv;
	set_err(STUB_ERR);
	return -1;
}

int
zpool_destroy(zpool_handle_t *zph, const char *tag)
{
	(void)zph;
	(void)tag;
	set_err(STUB_ERR);
	return -1;
}

int
zpool_clear(zpool_handle_t *zph, const char *device, nvlist_t *load_policy)
{
	(void)zph;
	(void)device;
	(void)load_policy;
	set_err(STUB_ERR);
	return -1;
}

int
zpool_vdev_online(zpool_handle_t *zph, const char *path, int flags,
    vdev_state_t *newstate)
{
	(void)zph;
	(void)path;
	(void)flags;
	if (newstate != NULL)
		*newstate = VDEV_STATE_UNKNOWN;
	return 0;
}

int
zpool_vdev_offline(zpool_handle_t *zph, const char *path,
    boolean_t temporary)
{
	(void)zph;
	(void)path;
	(void)temporary;
	set_err(STUB_ERR);
	return -1;
}

int
zpool_vdev_fault(zpool_handle_t *zph, uint64_t guid, vdev_aux_t aux)
{
	(void)zph;
	(void)guid;
	(void)aux;
	set_err(STUB_ERR);
	return -1;
}

char *
zpool_vdev_name(libzfs_handle_t *hdl, zpool_handle_t *zph, nvlist_t *nv,
    int flags)
{
	(void)hdl;
	(void)zph;
	(void)nv;
	(void)flags;
	return NULL;
}

uint64_t
zpool_vdev_path_to_guid(zpool_handle_t *zph, const char *path)
{
	(void)zph;
	(void)path;
	return 0;
}

int
zpool_export(zpool_handle_t *zph, boolean_t force, const char *tag)
{
	(void)zph;
	(void)force;
	(void)tag;
	set_err(STUB_ERR);
	return -1;
}

int
zpool_export_force(zpool_handle_t *zph, const char *tag)
{
	(void)zph;
	(void)tag;
	set_err(STUB_ERR);
	return -1;
}

int
zpool_initialize(zpool_handle_t *zph, pool_initialize_func_t func,
    nvlist_t *argv)
{
	(void)zph;
	(void)func;
	(void)argv;
	set_err(STUB_ERR);
	return -1;
}

int
zpool_import_props(libzfs_handle_t *hdl, nvlist_t *nv, const char *pool,
    nvlist_t *props, int flags)
{
	(void)hdl;
	(void)nv;
	(void)pool;
	(void)props;
	(void)flags;
	set_err(STUB_ERR);
	return -1;
}

zpool_status_t
zpool_import_status(nvlist_t *nv, const char **status,
    zpool_errata_t *errata)
{
	(void)nv;
	if (status != NULL)
		*status = "stub";
	if (errata != NULL)
		*errata = ZPOOL_ERRATA_NONE;
	return ZPOOL_STATUS_OK;
}

int
zpool_refresh_stats(zpool_handle_t *zph, boolean_t *missing)
{
	(void)zph;
	if (missing != NULL)
		*missing = 0;
	set_err(STUB_ERR);
	return -1;
}

int
zpool_disable_datasets(zpool_handle_t *zph, boolean_t recursive)
{
	(void)zph;
	(void)recursive;
	set_err(STUB_ERR);
	return -1;
}
