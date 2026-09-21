// Copyright 2026 dadadah
//
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/julienschmidt/httprouter"

	zfs "github.com/bicomsystems/go-libzfs"
)

// app bundles everything the HTTP handlers need.
type app struct {
	cfg   *Config
	state *stateStore
	// client has no overall timeout: transfers of large snapshots may run
	// for hours. Only the dial phase is bounded.
	client *http.Client
}

func main() {
	configPath := flag.String("config", "",
		"YAML config file (default: $ZFS_BACKUP_CONFIG, or config.yaml)")
	flag.Parse()

	if *configPath == "" {
		*configPath = os.Getenv("ZFS_BACKUP_CONFIG")
	}
	if *configPath == "" {
		*configPath = "config.yaml"
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("zfs-backup-service: %v", err)
	}

	st, err := newStateStore(cfg.StateDir)
	if err != nil {
		log.Fatalf("zfs-backup-service: state dir %q: %v", cfg.StateDir, err)
	}

	a := &app{
		cfg:   cfg,
		state: st,
		client: &http.Client{
			Transport: &http.Transport{
				DialContext: (&net.Dialer{Timeout: 15 * time.Second}).DialContext,
			},
		},
	}

	a.checkReceiveTarget()

	r := httprouter.New()
	r.NotFound = http.HandlerFunc(handleNotFound)
	r.GET("/", a.handleIndex)
	r.GET("/health", a.handleHealth)
	// Catch-all: the snapshot spec is a ZFS dataset path and contains
	// slashes (pool/dataset[@snapshot]).
	r.POST("/send/peers/:peer/snapshot/*snap", a.handleSend)
	r.POST("/receive/*ds", a.handleReceive)

	srv := &http.Server{Addr: cfg.Listen, Handler: r}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("zfs-backup-service listening on %s | receive target: %s | peers: %v",
			cfg.Listen, cfg.ReceiveTarget, cfg.peerIDs())
		errCh <- srv.ListenAndServe()
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-errCh:
		log.Fatalf("zfs-backup-service: server: %v", err)
	case <-ctx.Done():
		log.Printf("shutdown signal received, draining connections (up to 10s)...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("graceful shutdown: %v", err)
		}
		log.Printf("bye")
	}
}

// checkReceiveTarget verifies that the configured receive target dataset is
// usable, creating it when possible. A failure is only a warning: the node
// can still send backups to peers until the target is fixed.
func (a *app) checkReceiveTarget() {
	target := a.cfg.ReceiveTarget
	if ds, err := zfs.DatasetOpenSingle(target); err == nil {
		ds.Close()
		return
	}
	log.Printf("WARNING: receive target %q is not an open dataset, attempting to create it", target)
	if ds, err := zfs.DatasetCreate(target, zfs.DatasetTypeFilesystem, nil); err != nil {
		log.Printf("WARNING: could not create receive target %q: %v (incoming transfers will fail until it exists)", target, err)
	} else {
		ds.Close()
		log.Printf("created receive target dataset %q", target)
	}
}

// handleIndex describes the service and its endpoints.
func (a *app) handleIndex(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	a.writeJSON(w, http.StatusOK, map[string]any{
		"service":        "zfs-backup-service",
		"listen":         a.cfg.Listen,
		"receive_target": a.cfg.ReceiveTarget,
		"peers":          a.cfg.peerIDs(),
		"endpoints": map[string]string{
			"POST /send/peers/<peer>/snapshot/<pool/ds[@snap]>": "back up a snapshot (created on demand) to the named peer; ?from=<snap> sets an explicit incremental baseline, ?full=true forces a full send, ?recursive=true sends the whole subtree (zfs send -R), ?raw=true sets the raw send bundle (zfs send -w)",
			"POST /receive/<pool/ds...>":                        "internal: receive a snapshot stream from another node",
			"GET /health":                                       "liveness probe",
		},
	})
}

// handleHealth is a liveness probe.
func (a *app) handleHealth(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	a.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleNotFound returns a JSON 404 instead of httprouter's plain text.
func handleNotFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": fmt.Sprintf("no route for %s %s", r.Method, r.URL.Path),
	})
}

// writeJSON encodes v as a JSON response with the given status code.
func (a *app) writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writing response: %v", err)
	}
}

// writeError sends a JSON error response.
func (a *app) writeError(w http.ResponseWriter, code int, format string, args ...any) {
	a.writeJSON(w, code, map[string]string{"error": fmt.Sprintf(format, args...)})
}
