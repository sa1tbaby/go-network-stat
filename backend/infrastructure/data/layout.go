// Package data defines the single writable tree shared by application services.
package data

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sa1tbaby/go-network-stat/backend/infrastructure/fileutil"
)

// Layout separates configuration, process state, logs and measurement history.
// Source assets and release binaries never belong in this tree.
type Layout struct {
	Root       string
	Config     string
	Runtime    string
	Logs       string
	Statistics string
	Cache      string
	Build      string
	UDP        string
}

func New(root string) (Layout, error) {
	if !filepath.IsAbs(root) {
		return Layout{}, errors.New("data directory must be absolute")
	}

	root = filepath.Clean(root)

	return Layout{
		Root:       root,
		Config:     filepath.Join(root, "config"),
		Runtime:    filepath.Join(root, "runtime"),
		Logs:       filepath.Join(root, "logs"),
		Statistics: filepath.Join(root, "statistics"),
		Cache:      filepath.Join(root, "cache"),
		Build:      filepath.Join(root, "build"),
		UDP:        filepath.Join(root, "udp"),
	}, nil
}

func (l Layout) Create() error {
	for _, dir := range []string{l.Root, l.Config, l.Runtime, l.Logs, l.Statistics, l.Cache, l.Build, l.UDP} {
		if err := fileutil.NoLinks(dir); err != nil {
			return err
		}

		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("create data directory: %w", err)
		}
	}

	probe, err := os.CreateTemp(l.Root, ".write-check-")
	if err != nil {
		return err
	}

	name := probe.Name()
	if err := probe.Close(); err != nil {
		os.Remove(name)

		return err
	}

	return os.Remove(name)
}
