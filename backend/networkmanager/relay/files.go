package relay

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sa1tbaby/go-network-stat/backend/infrastructure/fileutil"
)

func randomID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		panic(err)
	}

	return hex.EncodeToString(value)
}

func writeJSON(path string, value any) error {
	for part := filepath.Clean(path); ; part = filepath.Dir(part) {
		info, err := os.Lstat(part)
		if err != nil && !os.IsNotExist(err) {
			return err
		}

		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("capture path is a link: %s", part)
		}

		if filepath.Dir(part) == part {
			break
		}
	}

	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}

	file, err := os.CreateTemp(filepath.Dir(path), ".relay-*.tmp")
	if err != nil {
		return err
	}

	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(data); err != nil {
		file.Close()

		return err
	}

	if err = file.Sync(); err != nil {
		file.Close()

		return err
	}

	if err = file.Close(); err != nil {
		return err
	}

	return fileutil.Replace(name, path)
}
