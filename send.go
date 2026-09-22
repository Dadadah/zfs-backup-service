// Copyright 2026 dadadah
//
// SPDX-License-Identifier: MIT

package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"

	zfs "github.com/bicomsystems/go-libzfs"
)

// handleSend implements:
//
//	POST /send/peers/<peer>/snapshot/<pool/dataset[@snapshot]>
//
// The last path segment names what to back up:
//   - "pool/dataset@snap"  sends that snapshot (created on demand)
//   - "pool/dataset"       takes a fresh snapshot (auto-named) and sends it
//
// If a previous snapshot of the dataset was already sent to this peer, the
// transfer is incremental (zfs send -i). Query parameters:
//
//	?full=true      force a full (non-incremental) send, e.g. after the
//	                peer's copies were wiped.
//	?from=snap      increment from an explicit previous snapshot instead
//	                of the one recorded in this service's state. This is
//	                how you adopt the service mid-history from an existing
//	                backup strategy that used a different snapshot naming
//	                scheme: the peer must already have that snapshot at the
//	                same dataset path, and the baseline is taken from it.
//	?recursive=true send the dataset recursively (zfs send -R: the
//	                snapshot and all descendant datasets). The receiving
//	                node stores the whole subtree under its receive_target.
//	?raw=true       raw send (zfs send -w): permit raw encrypted records —
//	                send -w in the zfs CLI sets raw, compress, embed_data
//	                and largeblock all at once; this parameter sets the same
//	                bundle. Needed when backing up encrypted datasets
//	                (e.g. without the key loaded on the sender).
func (a *app) handleSend(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	peerID := ps.ByName("peer")
	peer, ok := a.cfg.Peers[peerID]
	if !ok {
		a.writeError(w, http.StatusBadRequest,
			"unknown peer %q; configured peers: %v", peerID, a.cfg.peerIDs())
		return
	}

	// The catch-all delivers the remainder of the path; drop any leading
	// slash before parsing.
	spec := strings.TrimLeft(ps.ByName("snap"), "/")
	dsPath, snapName, err := parseSnapSpec(spec)
	if err != nil {
		a.writeError(w, http.StatusBadRequest, "%v", err)
		return
	}

	ds, err := zfs.DatasetOpenSingle(dsPath)
	if err != nil {
		a.writeError(w, http.StatusNotFound,
			"dataset %q not found: %v", dsPath, err)
		return
	}
	defer ds.Close()

	// Resolve the snapshot to send, creating it when it does not exist yet.
	created := false
	if snapName == "" {
		snapName, err = newAutoSnapName()
		if err != nil {
			a.writeError(w, http.StatusInternalServerError, "generating snapshot name: %v", err)
			return
		}
	}
	// Wire options for the send stream (mirrors the classic zfs send flags).
	// Parsed here because a recursive transfer needs a recursive snapshot:
	// zfs send -R sends the named snapshot across the whole subtree, so
	// every child dataset must carry a snapshot of that name.
	recursive := queryTrue(r, "recursive")
	raw := queryTrue(r, "raw")

	fullSnap := dsPath + "@" + snapName
	if ex, err := zfs.DatasetOpenSingle(fullSnap); err != nil {
		if _, err := zfs.DatasetSnapshot(fullSnap, recursive, nil); err != nil {
			a.writeError(w, http.StatusInternalServerError,
				"creating snapshot %s: %v%s", fullSnap, err, permissionHint(err))
			return
		}
		created = true
	} else {
		ex.Close()
	}

	// Choose the baseline for an incremental send. Precedence:
	//   1. explicit "?from=<snapshot>" — migrate from an existing backup
	//      strategy that used a different snapshot naming scheme; it
	//      overrides both the recorded state and ?full=true.
	//   2. the last snapshot this peer received from us (per-peer state).
	//   3. a full send.
	forceFull := r.URL.Query().Get("full") == "true"
	prev := ""
	fromSnapshot := ""
	if from := r.URL.Query().Get("from"); from != "" {
		snap, err := parseFromSpec(from, dsPath)
		if err != nil {
			a.writeError(w, http.StatusBadRequest, "%v", err)
			return
		}
		if ex, err := zfs.DatasetOpenSingle(dsPath + "@" + snap); err != nil {
			a.writeError(w, http.StatusNotFound,
				"baseline snapshot %s@%s not found on this dataset", dsPath, snap)
			return
		} else {
			ex.Close()
			prev = snap
			fromSnapshot = dsPath + "@" + snap
		}
	} else if !forceFull {
		if last, ok := a.state.load(peerID)[dsPath]; ok {
			if ex, err := zfs.DatasetOpenSingle(dsPath + "@" + last); err == nil {
				prev = last
				fromSnapshot = dsPath + "@" + last
				ex.Close()
			}
		}
	}

	// libzfs send flags for the stream (wire options parsed above).
	sendFlags := zfs.SendFlags{}
	if recursive {
		sendFlags.Replicate = true // -R: recursive (all descendant datasets)
	}
	if raw {
		// The zfs CLI's -w sets all of these at once (OpenZFS 2.3):
		sendFlags.Raw = true
		sendFlags.Compress = true
		sendFlags.EmbedData = true
		sendFlags.LargeBlock = true
	}

	snap, err := zfs.DatasetOpenSingle(fullSnap)
	if err != nil {
		a.writeError(w, http.StatusInternalServerError, "opening snapshot %s: %v", fullSnap, err)
		return
	}
	defer snap.Close()

	log.Printf("sending %s to peer %q (%s)%s%s%s",
		fullSnap, peerID, peer.Addr(), incrementalLabel(prev),
		flagLabel("recursive", recursive), flagLabel("raw", raw))
	if err := a.streamToPeer(r, peerID, peer.Addr(), dsPath, snapName, prev, sendFlags, snap); err != nil {
		code := http.StatusBadGateway
		var tf *transferFailure
		if errors.As(err, &tf) {
			code = tf.status
		}
		a.writeError(w, code, "%v", err)
		return
	}

	a.writeJSON(w, http.StatusOK, map[string]any{
		"status":           "ok",
		"peer":             peerID,
		"dataset":          dsPath,
		"snapshot":         fullSnap,
		"created_snapshot": created,
		"incremental":      prev != "",
		// The baseline the transfer started from ("ds@snap"); empty for a
		// full send. Useful to verify a ?from= migration baseline.
		"incremental_from": fromSnapshot,
		"recursive":        recursive,
		"raw":              raw,
	})
}

// queryTrue reports whether the query parameter name is set to a true-ish
// value ("true", "1", "yes", case-insensitive).
func queryTrue(r *http.Request, name string) bool {
	switch strings.ToLower(r.URL.Query().Get(name)) {
	case "true", "1", "yes":
		return true
	}
	return false
}

func flagLabel(name string, on bool) string {
	if on {
		return " [" + name + "]"
	}
	return ""
}

// transferFailure marks a failed transfer with the status code the handler
// should report to the caller (502 for peer-side failures, 500 for local
// send failures).
type transferFailure struct {
	status int
	err    error
}

func (t *transferFailure) Error() string { return t.err.Error() }

// streamToPeer streams the snapshot to the peer's /receive endpoint and
// records incremental state on success. The zfs send stream is piped
// straight through the HTTP body — nothing is buffered on disk.
func (a *app) streamToPeer(r *http.Request, peerID, addr, dsPath, snapName, prev string, flags zfs.SendFlags, snap zfs.Dataset) error {
	pr, pw, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("creating pipe: %w", err)
	}

	// Run the send in a goroutine: the HTTP client reads from the pipe, so
	// both must proceed concurrently.
	sendErr := make(chan error, 1)
	go func() {
		var err error
		if prev != "" {
			err = snap.SendFrom(dsPath+"@"+prev, pw, flags)
		} else {
			err = snap.Send(pw, flags)
		}
		pw.Close()
		sendErr <- err
	}()

	peerURL := fmt.Sprintf("http://%s/receive/%s", addr, dsPath)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, peerURL, pr)
	if err != nil {
		pr.Close()
		<-sendErr
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-zfs-stream")
	req.Header.Set("X-Zfs-Source-Snapshot", dsPath+"@"+snapName)

	resp, httpErr := a.client.Do(req)
	if httpErr != nil {
		pr.Close() // unblock the sender goroutine
		<-sendErr
		return &transferFailure{status: http.StatusBadGateway,
			err: fmt.Errorf("transferring to %s: %w", addr, httpErr)}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))

	// The response is in, so the body is fully consumed: our own receiver
	// only answers after it has read the whole stream. Close the pipe read
	// end anyway (a rejecting third party would not have consumed it) and
	// wait for the sender goroutine to finish.
	pr.Close()
	serr := <-sendErr

	if resp.StatusCode/100 != 2 {
		msg := strings.TrimSpace(string(body))
		if serr != nil {
			return &transferFailure{status: http.StatusBadGateway,
				err: fmt.Errorf("transfer to %s failed (local send: %v; peer: HTTP %d %s)",
					peerID, serr, resp.StatusCode, msg)}
		}
		return &transferFailure{status: http.StatusBadGateway,
			err: fmt.Errorf("peer %s rejected the stream: HTTP %d %s",
				peerID, resp.StatusCode, msg)}
	}
	if serr != nil {
		return &transferFailure{status: http.StatusInternalServerError,
			err: fmt.Errorf("zfs send failed after the peer acknowledged the stream: %v", serr)}
	}

	// Record state so the next send of this dataset is incremental.
	if err := a.state.mark(peerID, dsPath, snapName); err != nil {
		log.Printf("WARNING: could not save incremental state for peer %q: %v", peerID, err)
	}
	return nil
}

// parseSnapSpec splits the user-supplied spec into dataset path and snapshot
// name. Accepted forms:
//
//	"pool/dataset@snap" -> ("pool/dataset", "snap")
//	"pool/dataset"      -> ("pool/dataset", "")
func parseSnapSpec(spec string) (ds, snap string, err error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", "", errors.New("snapshot spec is empty")
	}
	at := strings.IndexByte(spec, '@')
	if at < 0 {
		ds = spec
	} else {
		ds, snap = spec[:at], spec[at+1:]
	}
	if !strings.Contains(ds, "/") {
		return "", "", fmt.Errorf("dataset %q must be a full path (pool/dataset)", ds)
	}
	if at >= 0 && snap == "" {
		return "", "", errors.New("invalid snapshot spec: empty snapshot name after '@'")
	}
	return ds, snap, nil
}

// parseFromSpec normalizes the ?from= value into a bare snapshot name that
// must live on dsPath (zfs send -i can only start from a snapshot of the
// same filesystem). Accepted forms:
//
//	"snap"         -> "snap" (assumed to be on dsPath)
//	"@snap"        -> "snap"
//	"pool/ds@snap" -> "snap" (rejected unless pool/ds == dsPath)
func parseFromSpec(from, dsPath string) (string, error) {
	from = strings.TrimSpace(from)
	if from == "" {
		return "", errors.New("?from= is empty")
	}
	if strings.HasPrefix(from, "@") {
		if snap := from[1:]; snap != "" {
			return snap, nil
		}
		return "", errors.New("?from= is empty")
	}
	if at := strings.LastIndexByte(from, '@'); at >= 0 {
		ds, snap := from[:at], from[at+1:]
		if snap == "" {
			return "", fmt.Errorf("invalid ?from= %q: empty snapshot name", from)
		}
		if ds != dsPath {
			return "", fmt.Errorf("?from= snapshot %q is on %q, not on %q", from, ds, dsPath)
		}
		return snap, nil
	}
	// No '@': treat as a bare snapshot name on dsPath. A '/' would make it
	// a dataset path, which is not a valid baseline.
	if strings.Contains(from, "/") {
		return "", fmt.Errorf("?from= %q looks like a dataset path; expected a snapshot name on %q", from, dsPath)
	}
	return from, nil
}

// newAutoSnapName builds a unique snapshot name like
// "auto-20260920-183000-a1b2c3d4".
func newAutoSnapName() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return time.Now().UTC().Format("auto-20060102-150405") + "-" + hex.EncodeToString(b[:]), nil
}

func incrementalLabel(prev string) string {
	if prev == "" {
		return " [full send]"
	}
	return fmt.Sprintf(" [incremental since %s]", prev)
}
