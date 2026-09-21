// Copyright 2026 dadadah
//
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

// stateStore tracks, per peer, the last snapshot that was successfully sent
// for each dataset. This is what makes repeated sends incremental: the next
// send of a dataset starts from the last snapshot the peer already has.
//
// State is kept as one JSON file per peer (a dataset -> snapshot map) in
// cfg.StateDir, e.g.:
//
//	office.json: {"tank/data": "auto-20260920-183000-a1b2c3d4"}
type stateStore struct {
	dir string
	mu  sync.Mutex
}

var stateFileNameRe = regexp.MustCompile(`[^a-zA-Z0-9._-]`)

func newStateStore(dir string) (*stateStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &stateStore{dir: dir}, nil
}

func (s *stateStore) file(peerID string) string {
	safe := stateFileNameRe.ReplaceAllString(peerID, "_")
	return filepath.Join(s.dir, safe+".json")
}

// load returns the last-sent-snapshot map for a peer. A missing or corrupt
// file yields an empty map (a fresh start means one full send, not a
// failure).
func (s *stateStore) load(peerID string) map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked(peerID)
}

func (s *stateStore) loadLocked(peerID string) map[string]string {
	st := map[string]string{}
	data, err := os.ReadFile(s.file(peerID))
	if err != nil {
		return st
	}
	if err := json.Unmarshal(data, &st); err != nil {
		// Corrupt state: ignore it rather than failing the transfer.
		return map[string]string{}
	}
	return st
}

// mark records that snapshot snap of dataset ds was successfully sent to
// peerID.
func (s *stateStore) mark(peerID, ds, snap string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.loadLocked(peerID)
	st[ds] = snap
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.file(peerID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.file(peerID))
}
