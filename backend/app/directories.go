package app

import (
	"os"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/storage"
)

func prepareDirectories(cfg Config) error {
	for _, directory := range []string{cfg.StateDirectory, cfg.RuntimeDirectory, cfg.LogDirectory} {
		if _, err := storage.FullPath(directory); err != nil {
			return err
		}

		if err := storage.NoLink(directory); err != nil {
			return err
		}

		if err := os.MkdirAll(directory, 0700); err != nil {
			return err
		}
	}

	return nil
}
