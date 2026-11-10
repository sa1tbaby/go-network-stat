package assets

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var assetNames = []string{"dashboard.html", "official-relays.json", "cod-test-targets.json"}

type Payload struct {
	Root string
}

// Prepare extracts the embedded panel/catalogs once per asset build into the
// dedicated data cache. User-selected measurements never enter this archive.
func Prepare(cache string) (Payload, error) {
	if !filepath.IsAbs(cache) {
		return Payload{}, errors.New("cache directory must be absolute")
	}

	archive, err := bundledArchive()
	if err != nil {
		return Payload{}, err
	}

	return PrepareArchive(cache, archive)
}

// PrepareArchive validates and caches an application asset archive atomically.
func PrepareArchive(cache string, archive []byte) (Payload, error) {
	hash := sha256.Sum256(archive)
	version := hex.EncodeToString(hash[:])[:24]
	root := filepath.Join(cache, version)
	result := Payload{Root: filepath.Join(root, "app")}
	if validPayload(root, version) {
		return result, nil
	}

	if err := os.MkdirAll(cache, 0700); err != nil {
		return Payload{}, fmt.Errorf("creating application cache: %w", err)
	}

	stage, err := os.MkdirTemp(cache, ".extract-")
	if err != nil {
		return Payload{}, err
	}

	defer os.RemoveAll(stage)
	if err := extractPayload(stage, archive); err != nil {
		return Payload{}, fmt.Errorf("preparing embedded components: %w", err)
	}

	if err := os.WriteFile(filepath.Join(stage, ".complete"), []byte(version), 0600); err != nil {
		return Payload{}, err
	}

	if err := os.Rename(stage, root); err != nil {
		// A second instance may have completed the same extraction. Do not remove
		// or overwrite its files, or any cache directory belonging to another run.
		if validPayload(root, version) {
			return result, nil
		}

		return Payload{}, fmt.Errorf("finalizing component preparation: %w", err)
	}

	return result, nil
}

func validPayload(root, version string) bool {
	marker, err := os.ReadFile(filepath.Join(root, ".complete"))
	if err != nil || string(marker) != version {
		return false
	}

	for _, asset := range assetNames {
		name := "app/" + asset
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return false
		}
	}

	return true
}
