// Copyright 2026 dadadah
//
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// DefaultPort is the port a peer's HTTP service listens on unless the
	// peer config sets its own port.
	DefaultPort = 8080

	// DefaultListen is the local listen address if config.yaml has none.
	DefaultListen = ":8080"

	// DefaultStateDir is where per-peer incremental state is stored.
	DefaultStateDir = "state"
)

// PeerConfig describes one remote ZFS node. Hostname is the only required
// field; Port defaults to DefaultPort.
type PeerConfig struct {
	// Hostname is the (Tailscale) hostname of the peer's service.
	Hostname string `yaml:"hostname"`
	// Port is optional; defaults to DefaultPort when zero.
	Port int `yaml:"port"`
}

// Addr returns "hostname" or "hostname:port".
func (p PeerConfig) Addr() string {
	if p.Port == 0 {
		return p.Hostname
	}
	return fmt.Sprintf("%s:%d", p.Hostname, p.Port)
}

// Config is the service configuration loaded from a YAML file.
type Config struct {
	// Listen is the local address:port for the HTTP server.
	Listen string `yaml:"listen"`

	// ReceiveTarget is the dataset root where received backups are stored.
	// It must be a full dataset path (pool/name). A dataset sent as
	// "tank/data" is stored as "<receive_target>/tank/data".
	ReceiveTarget string `yaml:"receive_target"`

	// StateDir is the directory for per-peer incremental backup state.
	StateDir string `yaml:"state_dir"`

	// Peers maps a peer id (used in URLs) to its network location.
	Peers map[string]PeerConfig `yaml:"peers"`
}

// loadConfig reads and validates the YAML config file at path.
func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Listen == "" {
		c.Listen = DefaultListen
	}
	if c.StateDir == "" {
		c.StateDir = DefaultStateDir
	}
	if c.Peers == nil {
		c.Peers = map[string]PeerConfig{}
	}
	for id, p := range c.Peers {
		if p.Port == 0 {
			p.Port = DefaultPort
			c.Peers[id] = p
		}
	}
}

func (c *Config) validate() error {
	if c.ReceiveTarget == "" {
		return fmt.Errorf("config: receive_target is required — the dataset where received backups are stored, e.g. \"tank/backups\"")
	}
	if !strings.Contains(c.ReceiveTarget, "/") {
		return fmt.Errorf("config: receive_target %q must be a full dataset path (pool/name)", c.ReceiveTarget)
	}
	for id, p := range c.Peers {
		if p.Hostname == "" {
			return fmt.Errorf("config: peer %q has no hostname", id)
		}
		if p.Port < 1 || p.Port > 65535 {
			return fmt.Errorf("config: peer %q has invalid port %d", id, p.Port)
		}
	}
	return nil
}

// peerIDs returns the configured peer ids, sorted for stable output.
func (c *Config) peerIDs() []string {
	ids := make([]string, 0, len(c.Peers))
	for id := range c.Peers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
