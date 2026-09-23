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
* The service runs as **root or a normal user** — non-root needs the
  `zfs allow` delegated privileges described in
  [Running as a non-root user](#running-as-a-non-root-user)
* A private network path between the nodes (Tailscale recommended)

Distro packages for the dev libraries:

| Distro        | Package(s)                                          |
|---------------|-----------------------------------------------------|
| Debian/Ubuntu | `libzfslinux-dev` (older releases: `libzfs-dev` `libnvpair-dev` `libspl-dev`) |
| Fedora        | `libzfs6-devel` (OpenZFS 2.3.x) or `libzfs7-devel` (2.4.x) — see below |
| openSUSE      | `zfsutils` devel package (`libzfs-devel`)           |
| NixOS         | `pkgs.zfs` (unfree) with `dev` output               |

Plus a C compiler (`gcc`) and Go 1.21+ (`golang` on Fedora).

On Fedora, OpenZFS is not in the base repositories — add the [OpenZFS
project's repo](https://openzfs.github.io/openzfs-docs/Getting%20Started/Fedora/index.html)
first (e.g. Fedora 44), then install the devel package matching the
OpenZFS generation you run:

```sh
sudo dnf install -y https://zfsonlinux.org/fedora/zfs-release-3-1.fc44.noarch.rpm
sudo dnf install -y golang gcc libzfs6-devel   # 2.3.x; use libzfs7-devel for 2.4.x
```

(The devel package also pulls in the runtime libraries, and the `zfs`
package installs the kernel module via DKMS — keep the userspace and
kernel module on the same generation.)

## Building

```sh
make            # normal build (needs the OpenZFS dev packages above)
```

or directly:

```sh
go build -o zfs-backup-service .
```

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

Before receiving, the service ensures every dataset in the destination path
exists, **creating the destination itself when it is missing**. This is
required for non-root receives (the kernel's receive permission check runs
against the destination dataset and fails if it does not exist yet, so a
full stream could never start); when running as root it is a harmless no-op
in the common case, and a full stream is applied over the empty dataset via
`-F`.

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

The service runs on a machine where the target pool is imported. It works
as root or as a normal user with the delegated privileges from the next
section. A minimal systemd unit (non-root):

```ini
[Unit]
Description=zfs-backup-service
After=network-online.target zfs-import.service
Wants=network-online.target

[Service]
ExecStart=/opt/zfs-backup-service/zfs-backup-service -config /opt/zfs-backup-service/config.yaml
Restart=on-failure
User=zfs-backup

[Install]
WantedBy=multi-user.target
```

(Use `User=root` instead if you prefer to run it privileged — no
delegated-privilege setup is needed then.)

## Running as a non-root user

All ZFS operations the service performs are covered by a small set of
delegated privileges, so it can run as an ordinary user (the example uses
`zfs-backup`; any user or group works). Run the following **once, as
root**, before starting the service.

### Prerequisites on the receiving node

```sh
# 1. Create the receive target once. The service never creates top-level
#    datasets — everything it creates lives *under* this one.
zfs create tank/backups

# 2. PREREQUISITE: the receive target subtree must stay unmounted.
#    The service never mounts (it receives with -u) and never unmounts,
#    and a forced overwrite (-F) cannot destroy a mounted dataset.
#    canmount=off is hereditary, so this covers every dataset the
#    service will create underneath.
zfs set canmount=off tank/backups

# 3. Delegate the privileges the service needs, on the receive target.
#    Dataset-level grants are inherited by all child datasets, so this
#    covers everything received under it (including recursive -R trees).
zfs allow zfs-backup create,mount,snapshot,receive tank/backups
```

Why exactly these (OpenZFS 2.3 security-policy checks):

| Permission | Needed for |
|---|---|
| `receive` | `zfs receive` itself |
| `snapshot` | receiving creates snapshots in the destination (incremental receives) |
| `create` | creating intermediate datasets and the destination before a receive (the check runs on the *parent*) |
| `mount` | the `create` and `receive` checks require it to be held. It is **not used** — on Linux a non-root user still cannot actually mount, which is fine because the subtree stays unmounted (step 2) |

No pool-level privilege is needed: the service only touches datasets under
`receive_target`.

### Prerequisites on the sending node

```sh
# Per dataset you back up. The grant is inherited by child datasets, which
# is what makes recursive (?recursive=true, zfs send -R) transfers work.
# (To cover an entire pool: zfs allow zfs-backup snapshot,send tank)
zfs allow zfs-backup snapshot,send tank/data
```

| Permission | Needed for |
|---|---|
| `snapshot` | creating the auto-named snapshot (recursively, for `-R` transfers) |
| `send` | streaming the snapshot to the peer |

### Verifying

```sh
sudo -u zfs-backup zfs allow tank/backups    # show the effective grants
sudo -u zfs-backup zfs allow tank/data       # (sending node)
```

The service logs its privilege mode at startup (a notice when it is not
running as root), and ZFS permission errors in HTTP responses carry a hint
pointing back to this section.

At startup the service also runs a **self-check** and refuses to start when
it could not possibly work: when `/dev/zfs` is missing or not accessible,
when the receive target cannot be opened, or — as a non-root user — when
the delegated permissions required for receiving (`receive,snapshot,create,
mount`) are missing on the receive target (it prints the exact `zfs allow`
command to fix it).

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
* When running as a non-root user, keep the receive target subtree
  unmounted (`canmount=off`, see [Running as a non-root user](#running-as-a-non-root-user))
  — the service never mounts or unmounts, and a forced overwrite cannot
  destroy a mounted dataset.

## Running in Docker

The container is a **thin client to the host's ZFS kernel module**: it
only issues dataset-level ioctls on `/dev/zfs` and never mounts anything,
so no kernel module and no pool filesystem is needed inside the image —
just the host device passed in and the OpenZFS userspace libraries
(bundled). The host must have the OpenZFS module loaded and the pool
imported.

### Prerequisites (on the host, once, as root)

Identical to [Running as a non-root user](#running-as-a-non-root-user) —
the receive target must exist with `canmount=off`, and the `zfs allow`
grants must be in place. The one difference: the grants go to **the UID
the container process has on the host**, not to a username. With
rootless Docker that is the uid of the user running Docker (or the one set
via `user:` in the compose file); with `--userns-remap` it is the mapped
host uid. Verify with `docker exec zfs-backup id`.

```sh
# Receiving node:
zfs create   tank/backups
zfs set      canmount=off tank/backups
zfs allow    <host-uid> create,mount,snapshot,receive tank/backups

# Sending node (per dataset you back up):
zfs allow    <host-uid> snapshot,send tank/data
```

### Build and run

```sh
docker build -t zfs-backup-service .
```

**Posture B (default — no root, no extra capabilities):** the container
runs as an ordinary user and relies on the delegated privileges above.
The included `docker-compose.yml` (with `config.docker.yaml` as the config
example) is set up for this; `user: "1000:1000"` must be the host
identity that received the grants:

```sh
cp config.docker.yaml config.docker.yaml.local   # and edit it
docker compose up -d
```

or without compose:

```sh
docker run -d --name zfs-backup \
    --user 1000:1000 \
    --device /dev/zfs \
    -v $PWD/config.docker.yaml:/etc/zfs-backup/config.yaml:ro \
    -v zfs-backup-state:/var/lib/zfs-backup \
    -p 127.0.0.1:8080:8080 \
    zfs-backup-service
```

**Posture A (fallback — rootful, `CAP_SYS_ADMIN`):** if you would rather
not set up delegated privileges, run the container as root with
`--cap-add SYS_ADMIN` (and no `--user`) instead. The kernel then
authorizes every operation via the capability, and no `zfs allow` grants
are needed. This works, but note the container's root *is* the host's
root — it is far less isolated than posture B and is only recommended if
isolation is not a concern.

The **startup self-check** covers the common container misconfigurations:
a missing `--device /dev/zfs`, an unusable device, or missing delegated
grants all produce a clear, actionable fatal message at boot (the service
refuses to start rather than serving broken transfers).

### Tailscale in the container

Either run `tailscaled` as a sidecar container (needs `/dev/net/tun` and
`NET_ADMIN`, sharing a network namespace with the service) or run the
service with `network_mode: host` and tailscaled on the host.

### Limitations

* The container runs the backup service; it does not give you mounted
  access to the received backups — inspect them on the host.
* The image is built against OpenZFS 2.2 userspace (Ubuntu 24.04 base);
  the host should run a compatible 2.2+ kernel module.
* Everything else in [Operational notes](#operational-notes) applies
  unchanged.

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
