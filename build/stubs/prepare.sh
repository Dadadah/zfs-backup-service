#!/bin/sh
#
# Copyright 2026 dadadah
#
# SPDX-License-Identifier: MIT
#
# Prepare the stub build environment used by `make stub-build`:
#  - mirrors the OpenZFS 2.3.x header layout into .stub-build/include
#  - compiles build/stubs/stub_zfs.c into fake libzfs/libzpool/libnvpair/
#    libzfs_core shared libraries in .stub-build/lib
#
# This lets `go build` compile the full cgo binary on machines WITHOUT a
# ZFS installation (headers + libraries). The resulting binary is a "smart"
# fake: dataset open/snapshot/create succeed, and zfs_send/zfs_receive
# exchange a marker stream, so the full send -> HTTP pipe -> receive
# pipeline can be exercised between two instances (set
# ZFS_STUB_STREAM_BYTES=N to pad the stream to N bytes). Operations
# outside that pipeline fail with a "stub" error.
#
# Usage: build/stubs/prepare.sh [path-to-openzfs-source-tree]
#   Without an argument it downloads OpenZFS zfs-2.3.1 source (headers only
#   are used) into .stub-build/.
set -eu

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OUT="$ROOT/.stub-build"
SRC="${1:-}"

if [ -z "$SRC" ]; then
	SRC="$OUT/openzfs-src"
	if [ ! -f "$SRC/include/libzfs.h" ]; then
		mkdir -p "$OUT"
		echo "downloading OpenZFS zfs-2.3.1 source..."
		curl -fsSL -o "$OUT/zfs-2.3.1.tar.gz" \
			"https://codeload.github.com/openzfs/zfs/tar.gz/refs/tags/zfs-2.3.1"
		tar xzf "$OUT/zfs-2.3.1.tar.gz" -C "$OUT"
		mv "$OUT/zfs-zfs-2.3.1" "$SRC"
	fi
fi

if [ ! -f "$SRC/include/libzfs.h" ]; then
	echo "error: $SRC does not look like an OpenZFS source tree" >&2
	echo "       (missing include/libzfs.h)" >&2
	exit 1
fi

# The symlinks below embed these paths, so they must be absolute.
case "$SRC" in
	/*) : ;;
	*) SRC="$(pwd)/$SRC" ;;
esac

# --- 1) header include mirror (mimics the distro layout) -------------------
# Distro layout:  /usr/include/libzfs.h, /usr/include/libzfs/sys/*,
#                 /usr/include/sys/{fs/zfs.h,nvpair.h,...}
# The vendored go-libzfs needs both <libzfs.h> and <libzfs/sys/zfs_context.h>
# to resolve, plus the sys/ headers that live in the source tree.
INC="$OUT/include"
rm -rf "$INC"
mkdir -p "$INC"
ln -s "$SRC/include/sys" "$INC/sys"
ln -s "$SRC/include" "$INC/libzfs"
for h in "$SRC"/include/*.h; do
	ln -sf "$h" "$INC/"
done

# The libspl headers (libshare.h, umem.h, sys/uuid.h, ...) live outside
# include/ in the source tree; expose them under stable paths.
rm -rf "$OUT/libspl-include"
ln -s "$SRC/lib/libspl/include" "$OUT/libspl-include"

# --- 2) stub shared libraries ----------------------------------------------
LIB="$OUT/lib"
mkdir -p "$LIB"
CC="${CC:-cc}"
CFLAGS="-fPIC -shared -I$INC -I$SRC/lib/libspl/include/os/linux -I$SRC/lib/libspl/include -D_GNU_SOURCE"

# One stub object is used for every fake library; the linker only needs the
# symbols to resolve, and at runtime the stubs answer "not available".
OBJS="$OUT/stub.o"
rm -f "$OBJS"
"$CC" $CFLAGS -c "$ROOT/build/stubs/stub_zfs.c" -o "$OBJS"
for lib in libzfs.so libzpool.so libnvpair.so libzfs_core.so libtpool.so; do
	"$CC" -shared -o "$LIB/$lib" "$OBJS"
done

echo "stub build environment ready:"
echo "  includes: $INC"
echo "  libs:     $LIB"
