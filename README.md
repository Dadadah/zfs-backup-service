# zfs-backup-service

> [!IMPORTANT]
> Please note that this repo is not ready yet, do not run it in a production environment!

A small Go microservice for snapshotting ZFS datasets and shipping them to
another machine over a private network (intended: a Tailscale tailnet).
**Each ZFS node runs its own copy of the service.** One `curl` against the
local service starts a transfer to the remote copy:

```sh
curl -XPOST http://localhost:8080/send/peers/<peer id>/snapshot/<snapshot to back up>
```

Data moves as a plain `zfs send` stream piped straight through an HTTP POST
body — nothing is buffered on disk, no TLS and no authentication by design
(the service is meant to live inside a tailnet, reachable only from there).

```
node A (source)                              node B (target)
┌──────────────────────────────┐             ┌──────────────────────────────┐
│ zfs-backup-service :8080     │  zfs send   │ zfs-backup-service :8080     │
│                              │  stream     │                              │
│ POST /send/peers/b/snapshot/ ├────────────►│ POST /receive/tank/data      │
│   tank/data@snap             │  HTTP POST  │        (zfs receive)         │
│                              │             │ stored as                    │
│ state/b.json:                │             │ <receive_target>/tank/data   │
│   tank/data -> last snap     │             │                              │
└──────────────────────────────┘             └──────────────────────────────┘
```

## AI Disclosure

This project is written entirely with a locally hosted LLM. All contributions to it in the future may or may not be LLM written.

## Requirements

* Go 1.21+
* OpenZFS **2.2 or newer** userspace libraries + headers (the vendored
  `go-libzfs` is patched for the 2.x API — see
  [third_party/go-libzfs/PATCHES.md](third_party/go-libzfs/PATCHES.md))
* The service runs as **root** (it must create snapshots and receive datasets)
* A private network path between the nodes (Tailscale recommended)

Distro packages for the dev libraries:

| Distro        | Package(s)                                          |
|---------------|-----------------------------------------------------|
| Debian/Ubuntu | `libzfs-dev` `libnvpair-dev` `libspl-dev`           |
| Fedora        | `zfs-devel` (provides the `libzfs`/`libspl` headers)|
| openSUSE      | `zfsutils` devel package (`libzfs-devel`)           |
| NixOS         | `pkgs.zfs` (unfree) with `dev` output               |

## Building

```sh
make            # normal build (needs the OpenZFS dev packages above)
```

or directly:

```sh
go build -o zfs-backup-service .
```

### Building on a machine without ZFS (verification only)

If you just want to compile-check the code (or exercise the HTTP plumbing)
on a machine that has no ZFS at all, `make stub-build` downloads the pinned
OpenZFS source for its headers and builds tiny stub libraries that mimic the
API. The resulting `zfs-backup-service-stub` starts and serves HTTP, and the
send→pipe→receive pipeline works end-to-end between two instances (dataset
open/snapshot/create succeed in the stub; `zfs send`/`zfs receive` exchange
a marker stream — set `ZFS_STUB_STREAM_BYTES=N` to pad the stream to N
bytes, which is how the deadlock-free transfer path was tested). Operations
outside that pipeline fail with a "stub" error. See
[build/stubs/prepare.sh](build/stubs/prepare.sh).

## Configuration

Copy [config.yaml](config.yaml) and edit it. The file is passed via
`-config` (or the `ZFS_BACKUP_CONFIG` environment variable; defaults to
`config.yaml` in the working directory).

```yaml
# Local address:port for the HTTP API (default ":8080")
listen: ":8080"

# Dataset root where received backups are stored. Full dataset path
# (pool/name). Sent as "tank/data" -> stored as "tank/backups/tank/data".
receive_target: "tank/backups"

# Directory for per-peer incremental backup state (default "state")
state_dir: "state"

# Remote nodes. Only `hostname` is required; `port` defaults to 8080.
peers:
  office:
    hostname: "office.tail1234abcd.ts.net"
  home:
    hostname: "home.tail1234abcd.ts.net"
    port: 9090
```

| Key              | Default  | Meaning                                                                 |
|------------------|----------|-------------------------------------------------------------------------|
| `listen`         | `:8080`  | Local HTTP bind address                                                  |
| `receive_target` | *(required)* | Dataset root for incoming backups, full path `pool/name`           |
| `state_dir`      | `state`  | Where per-peer incremental state files live                              |
| `peers`          | —        | Map of `id -> {hostname, port?}`; the id is used in URLs                 |

The receive target dataset is created at startup if it does not exist yet
(the pool must).

## Usage

Back up a dataset (an auto-named snapshot is taken first if you don't name
one):

```sh
# snapshot of tank/data, auto-named like auto-20260921-024058-1eedd64f
curl -XPOST http://localhost:8080/send/peers/office/snapshot/tank/data

# a specific snapshot (created on demand if it does not exist)
curl -XPOST http://localhost:8080/send/peers/office/snapshot/tank/data@nightly
```

Successful response:

```json
{
  "status": "ok",
  "peer": "office",
  "dataset": "tank/data",
  "snapshot": "tank/data@auto-20260921-024058-1eedd64f",
  "created_snapshot": true,
  "incremental": false
}
```

### Incremental transfers

After a dataset has been sent to a peer once, the service remembers the
snapshot it sent (per peer, per dataset, in `<state_dir>/<peer>.json`). The
next send of the same dataset is **incremental** (`zfs send -i`) from that
snapshot — so repeat runs only transfer what changed.

Query parameters (combinable only as noted):

* `?full=true` — force a full (non-incremental) send, e.g. after the
  target's copies were wiped or moved:
  `curl -XPOST 'http://localhost:8080/send/peers/office/snapshot/tank/data?full=true'`
* `?from=<snapshot>` — increment from an *explicit* previous snapshot
  instead of the recorded state (see below). When both `?from=` and
  `?full=true` are given, `?from=` wins.
* `?recursive=true` — send the dataset **recursively** (`zfs send -R`):
  the snapshot plus all descendant datasets. The peer stores the whole
  subtree under its `receive_target`.
* `?raw=true` — **raw send** (`zfs send -w`): permit raw encrypted
  records. In the zfs CLI, `-w` sets `raw + compress + embed_data +
  largeblock` at once; this parameter sets the same bundle. Use it when
  backing up **encrypted datasets** (e.g. with the key not loaded on the
  sender). Without it, sending an encrypted dataset may require the key to
  be loaded and may fail.
* If the remembered snapshot no longer exists locally, the service falls
  back to a full send automatically.
* State is only written after the peer confirmed the transfer (HTTP 2xx),
  and the response reports the baseline used in `incremental_from`, plus
  `recursive` / `raw` echoing the wire options that were used.

#### Mirroring an existing `zfs send` / `zfs receive` pipeline

If your current strategy is a classic pipe, e.g.
`zfs send -Rwi ... | ssh peer 'zfs receive -Fuv ...'`, the mapping is:

| classic flag | in this service |
|--------------|-----------------|
| `send -i <from>` | incremental by default (per-peer state), or explicit `?from=<snap>` |
| `send -R` (recursive) | `?recursive=true` |
| `send -w` (raw) | `?raw=true` (same raw+compress+embed+large bundle) |
| `receive -F` (force) | always (receiving-node policy) |
| `receive -u` (no mount) | always |
| `receive -v` (verbose) | always (goes to the service log/journal) |
| — (extra) | `receive -s` (resumable) |

So the equivalent of your pipeline for one dataset is:

```sh
curl -XPOST 'http://localhost:8080/send/peers/office/snapshot/tank/data?recursive=true&raw=true'
```

#### Migrating from an existing backup strategy (`?from=`)

If the peer already holds backups made by another tool with a different
snapshot naming scheme, start the new service incrementally from one of
those snapshots instead of sending a full copy again:

```sh
# the peer already has tank/data@nightly-2026-09-19 (made by the old tool);
# take a fresh snapshot and send only the delta since nightly-2026-09-19
curl -XPOST 'http://localhost:8080/send/peers/office/snapshot/tank/data?from=nightly-2026-09-19'
```

Rules and caveats:

* The `from` snapshot must exist **locally** (it is the source of
  `zfs send -i`) and, per ZFS incremental-receive semantics, the peer must
  already have the **same snapshot name at the same dataset path** under its
  `receive_target` — otherwise the receive fails with "no such snapshot
  exists".
* The value may be a bare snapshot name (recommended), `@name`, or
  `pool/ds@name` (rejected unless it is the dataset being sent).
* `?from=` is a one-shot override: after the transfer succeeds, this
  service's state is updated to the snapshot that was just sent, so
  subsequent sends continue incrementally from there with no extra
  parameters.

### What happens on the receiving side

The sender POSTs the `zfs send` stream to the peer's
`POST /receive/<dataset path>`. The receiver writes it into
`<receive_target>/<dataset path>` (creating the intermediate datasets as
needed). Received datasets are **not mounted** automatically, and receives
use the *resumable* flag, so a transfer interrupted by a network drop can be
completed by simply re-running the same send.

The receiving node uses a fixed policy that mirrors a classic
`zfs receive -Fuv`, plus one extra:

| Flag | Effect |
|------|--------|
| `-F` | override the destination even if it already exists — re-running an interrupted or duplicate transfer succeeds |
| `-u` | do not mount the received dataset |
| `-v` | verbose progress (written to the service's log/journal, not the HTTP response) |
| `-s` | leave the destination resumable |

Receives are done into the **exact** destination path (no `-d`), so the
layout matches a classic `zfs send | zfs receive <path>` pipeline: a stream
of `pool/ds` lands at `receive_target/pool/ds`, and recursive (`-R`)
streams store the whole subtree there with the source pool name preserved.
(This deliberately avoids `-d`: with `-d`, ZFS appends only the stream path
*minus its pool name* to the prefix, so `pool/ds` would land at
`receive_target/ds` — same-named datasets in different source pools would
collide.)

Note the `-F` trade-off: if a destination dataset at the same path holds
*other* data and a non-applicable stream arrives, `-F` lets the receive
override it. The service assumes `receive_target` is dedicated to backups.

## HTTP API

| Method & path | Purpose |
|---|---|
| `POST /send/peers/:peer/snapshot/:dataset[@snap]` | Create (if needed) and send a snapshot to `:peer` (optional `?from=<snap>`, `?full=true`) |
| `POST /receive/:dataset...` | Internal endpoint used by other nodes — receives a snapshot stream |
| `GET /health` | Liveness probe (`{"status":"ok"}`) |
| `GET /` | Service description + configured peers |

Error responses are JSON: `{"error": "..."}` with the matching status code:
400 (bad request / unknown peer), 404 (dataset or route not found),
500 (local ZFS failure), 502 (peer unreachable or rejected the stream).

## Running it

The service needs to run as root and on a machine where the target pool is
imported. A minimal systemd unit:

```ini
[Unit]
Description=zfs-backup-service
After=network-online.target zfs-import.service
Wants=network-online.target

[Service]
ExecStart=/opt/zfs-backup-service/zfs-backup-service -config /opt/zfs-backup-service/config.yaml
Restart=on-failure
User=root

[Install]
WantedBy=multi-user.target
```

### Operational notes

* **No TLS, no authentication — by design.** Anyone who can reach the
  listening port can trigger sends and receive streams. The default
  `:8080` bind is `0.0.0.0`, which also exposes the API on your LAN, not
  just the tailnet. If that matters, bind `listen` to your Tailscale
  interface's IP (e.g. `100.x.y.z:8080`) or keep the port firewalled.
* Long transfers are normal: there is intentionally no HTTP timeout on
  send/receive requests (only the initial dial is bounded to 15 s).
* Keep the per-node `state_dir` stable across restarts; deleting a peer's
  state file simply makes the next send a full one.
* The service keeps the HTTP server alive even if the receive target dataset
  is missing (it logs a warning and keeps sending to peers); incoming
  transfers fail with a 500 until the target exists again.

## Dependencies

Per the project constraint, only three third-party modules are used:

* `github.com/julienschmidt/httprouter` — the HTTP router (required)
* `github.com/bicomsystems/go-libzfs` v0.4.0 — the ZFS bindings (required).
  The upstream package is unmaintained and does not compile against modern
  OpenZFS, so it is vendored in [`third_party/go-libzfs/`](third_party/go-libzfs/)
  with minimal, documented patches (see `third_party/go-libzfs/PATCHES.md`)
  and wired in via a `replace` directive in `go.mod`.
* `gopkg.in/yaml.v3` — required to parse the YAML config file.

## Project layout

```
main.go       entry point, HTTP server, routes, health/index handlers
config.go     YAML config loading + validation
send.go       POST /send/... — snapshot + stream a dataset to a peer
receive.go    POST /receive/... — accept a stream into receive_target
state.go      per-peer incremental state (JSON files)
build/stubs/  stub library toolchain for ZFS-less compile verification
third_party/  vendored, patched go-libzfs
```

## License

This project (everything except `third_party/`) is licensed under the MIT
License — see [LICENSE](LICENSE).

`third_party/go-libzfs` is a vendored, lightly patched copy of
[go-libzfs](https://github.com/bicomsystems/go-libzfs) by Faruk Kasumovic,
licensed under the [BSD-3-Clause License](third_party/go-libzfs/LICENSE.md)
(its own license applies to that subtree; local modifications are listed in
[third_party/go-libzfs/PATCHES.md](third_party/go-libzfs/PATCHES.md)).
