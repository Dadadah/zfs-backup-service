# Copyright 2026 dadadah
#
# SPDX-License-Identifier: MIT
#
# zfs-backup-service runs as a thin client to the HOST's ZFS kernel module:
# it only issues dataset-level ioctls on /dev/zfs (snapshot/send/receive/
# create) and never mounts anything. The container therefore needs the host
# device passed in (--device /dev/zfs) — see README "Running in Docker".

# ---------------------------------------------------------------- build ---
# OpenZFS headers come from libzfslinux-dev (OpenZFS 2.2 on Ubuntu 24.04,
# which satisfies the service's 2.2+ requirement). On this distro the
# headers are NOT flat in /usr/include but under /usr/include/{libzfs,libspl},
# hence the CGO_CFLAGS below (mirroring what a flat-layout distro gives for
# free).
FROM ubuntu:24.04 AS build

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        gcc \
        golang-go \
        libc6-dev \
        libzfslinux-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY go.mod go.sum ./
COPY third_party/ third_party/
RUN go mod download
COPY . .
RUN CGO_CFLAGS="-I/usr/include/libzfs -I/usr/include/libspl" \
    go build -trimpath -o /out/zfs-backup-service .

# -------------------------------------------------------------- runtime ---
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive
# Only the OpenZFS runtime libraries are needed (the ZFS kernel module runs
# on the host). zfsutils-linux is used as the source of those libraries; it
# also provides the zfs/zpool CLIs, handy for debugging inside the container.
RUN apt-get update && apt-get install -y --no-install-recommends \
        curl \
        zfsutils-linux \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/zfs-backup-service /usr/local/bin/zfs-backup-service

EXPOSE 8080
HEALTHCHECK --interval=1m --timeout=5s --start-period=10s \
    CMD curl -fsS http://127.0.0.1:8080/health || exit 1

ENTRYPOINT ["/usr/local/bin/zfs-backup-service"]
CMD ["-config", "/etc/zfs-backup/config.yaml"]
