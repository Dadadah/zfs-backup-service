GO   ?= go
BIN  := zfs-backup-service
SB   := .stub-build

.PHONY: all build stub-build stub-run clean

all: build

# Normal build: requires the OpenZFS userspace headers + libraries installed
# on this machine (e.g. "libzfs-dev" on Debian/Ubuntu, "zfs-devel" on
# Fedora). The binary must later be RUN as root; building it is unprivileged.
build:
	$(GO) build -o $(BIN) .

# Build for machines WITHOUT ZFS: prepare.sh downloads the OpenZFS source
# (headers only) and builds stub libzfs/libzpool/libnvpair libraries. The
# resulting binary is a "smart" fake — the full send -> HTTP pipe -> receive
# pipeline works between two instances (see build/stubs/prepare.sh and the
# README). Useful for compile checks and HTTP smoke tests.
stub-build:
	sh build/stubs/prepare.sh
	CGO_CFLAGS="-I$(CURDIR)/$(SB)/include -I$(CURDIR)/$(SB)/libspl-include/os/linux -I$(CURDIR)/$(SB)/libspl-include" \
	CGO_LDFLAGS="-L$(CURDIR)/$(SB)/lib -Wl,-rpath,$(CURDIR)/$(SB)/lib" \
	$(GO) build -o $(BIN)-stub .

# Run the stub binary with the example config (needs port 8080 free).
stub-run: stub-build
	./$(BIN)-stub -config config.yaml

clean:
	rm -f $(BIN) $(BIN)-stub
	rm -rf $(SB) state
