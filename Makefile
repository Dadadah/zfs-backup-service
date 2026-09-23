GO     ?= go
DOCKER ?= docker
BIN    := zfs-backup-service

.PHONY: all build docker-build clean

all: build

# Build: requires the OpenZFS userspace headers + libraries installed on
# this machine (e.g. "libzfslinux-dev" on Debian/Ubuntu, "libzfs6-devel" /
# "libzfs7-devel" on Fedora — see README "Requirements"). The service runs
# as root or as a non-root user with delegated privileges; building it is
# unprivileged.
build:
	$(GO) build -o $(BIN) .

# Build the Docker image (see README "Running in Docker").
docker-build:
	$(DOCKER) build -t zfs-backup-service .

clean:
	rm -f $(BIN)
	rm -rf state
