// Copyright 2026 dadadah
//
// SPDX-License-Identifier: MIT

package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/julienschmidt/httprouter"

	zfs "github.com/bicomsystems/go-libzfs"
)

// handleReceive implements:
//
//	POST /receive/<pool/dataset...>
//
// It is the counterpart of the send endpoint: another node streams a
// `zfs send` output as the request body, and this node writes it into
// <receive_target>/<pool/dataset...>. The body is piped straight into
// `zfs receive`; nothing is buffered on disk.
//
// The receive policy mirrors a classic `zfs receive -Fuv` plus one extra:
//
//	-F  override the destination even if it already exists — re-running an
//	    interrupted or duplicate transfer succeeds instead of erroring
//	-u  do not mount the received dataset
//	-v  verbose progress (written to this service's log/journal)
//	-s  (extra) leave the destination resumable
//
// The receive is done into the exact destination path (no -d): a stream of
// pool/ds lands at <receive_target>/pool/ds, exactly like a classic
// `zfs send | zfs receive <path>` pipeline. Recursive (zfs send -R)
// streams are received the same way — the named dataset at the exact path,
// all descendant datasets underneath it — preserving the source pool name
// in the layout (a -d receive would drop it, which can cause collisions
// between same-named datasets in different source pools).
func (a *app) handleReceive(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	dsPath := strings.TrimLeft(ps.ByName("ds"), "/")
	if dsPath == "" {
		a.writeError(w, http.StatusBadRequest,
			"missing dataset path; expected POST /receive/<pool>/<dataset>")
		return
	}
	dest := a.cfg.ReceiveTarget + "/" + dsPath
	log.Printf("receiving %s into %s from %s", dsPath, dest, r.RemoteAddr)

	// The receive target root must exist (it is created at startup).
	if rt, err := zfs.DatasetOpenSingle(a.cfg.ReceiveTarget); err != nil {
		a.writeError(w, http.StatusInternalServerError,
			"receive target %q is not an open dataset: %v", a.cfg.ReceiveTarget, err)
		return
	} else {
		rt.Close()
	}
	// Ensure every dataset in the destination path exists, including the
	// final one (see ensureDatasets).
	if err := ensureDatasets(a.cfg.ReceiveTarget, dsPath); err != nil {
		a.writeError(w, http.StatusInternalServerError,
			"preparing destination %q: %v%s", dest, err, permissionHint(err))
		return
	}

	pr, pw, err := os.Pipe()
	if err != nil {
		a.writeError(w, http.StatusInternalServerError, "creating pipe: %v", err)
		return
	}

	// Feed the request body into the pipe; zfs receive reads the other end.
	copyErr := make(chan error, 1)
	go func() {
		_, cerr := io.Copy(pw, r.Body)
		pw.Close()
		copyErr <- cerr
	}()

	rerr := zfs.ReceiveInto(dest, pr, zfs.RecvFlags{
		Force:     true, // -F
		NoMount:   true, // -u
		Verbose:   true, // -v
		Resumable: true, // -s
	})
	pr.Close() // unblocks the copy goroutine when receive stops early
	cerr := <-copyErr

	if rerr != nil {
		if cerr != nil {
			a.writeError(w, http.StatusInternalServerError,
				"zfs receive failed (stream: %v; client: %v)", rerr, cerr)
		} else {
			a.writeError(w, http.StatusInternalServerError,
				"zfs receive failed: %v%s", rerr, permissionHint(rerr))
		}
		return
	}
	if cerr != nil {
		a.writeError(w, http.StatusInternalServerError, "client disconnected mid-stream: %v", cerr)
		return
	}

	log.Printf("received %s into %s", dsPath, dest)
	a.writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"dataset":  dest,
		"snapshot": r.Header.Get("X-Zfs-Source-Snapshot"),
	})
}

// ensureDatasets opens every dataset between root and root/<dsPath>,
// creating any that are missing — including the final destination itself.
// Pre-creating the destination is required for non-root receives: the
// kernel's receive permission check runs against the destination dataset and
// fails if it does not exist yet, so a full stream could never start for a
// non-root user. When running as root this is a harmless no-op in the
// common (already exists) case, and a full stream is then applied over the
// freshly created empty dataset via -F.
func ensureDatasets(root, dsPath string) error {
	parts := strings.Split(dsPath, "/")
	for i := 1; i <= len(parts); i++ {
		p := root + "/" + strings.Join(parts[:i], "/")
		if ex, err := zfs.DatasetOpenSingle(p); err == nil {
			ex.Close()
			continue
		}
		ds, err := zfs.DatasetCreate(p, zfs.DatasetTypeFilesystem, nil)
		if err != nil {
			return err
		}
		ds.Close()
		if i < len(parts) {
			log.Printf("created intermediate dataset %q", p)
		} else {
			log.Printf("created destination dataset %q", p)
		}
	}
	return nil
}
