package assets

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/sa1tbaby/go-network-stat/frontend"
)

func bundledArchive() ([]byte, error) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, name := range assetNames {
		data, err := frontend.Files.ReadFile(name)
		if err != nil {
			return nil, err
		}

		entry, err := writer.Create("app/" + name)
		if err != nil {
			return nil, err
		}

		if _, err := entry.Write(data); err != nil {
			return nil, err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	return buffer.Bytes(), nil
}

func extractPayload(root string, archive []byte) error {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return err
	}

	if len(reader.File) > 20000 {
		return errors.New("too many component files")
	}

	var total uint64
	seen := make(map[string]bool)
	for _, item := range reader.File {
		name := item.Name
		if strings.Contains(name, "\\") || strings.Contains(name, ":") || strings.IndexByte(name, 0) >= 0 || strings.HasPrefix(name, "/") {
			return fmt.Errorf("invalid component name %q", name)
		}

		clean := filepath.Clean(filepath.FromSlash(name))
		if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
			return fmt.Errorf("component outside cache: %q", name)
		}

		if !strings.HasPrefix(name, "app/") {
			return fmt.Errorf("unknown component group: %q", name)
		}

		if item.Mode()&os.ModeSymlink != 0 || (!item.Mode().IsRegular() && !item.FileInfo().IsDir()) {
			return fmt.Errorf("link or special file in components: %q", name)
		}

		key := strings.ToLower(clean)
		if seen[key] {
			return fmt.Errorf("duplicate component: %q", name)
		}

		seen[key] = true
		total += item.UncompressedSize64
		if total > 1024*1024*1024 || item.UncompressedSize64 > 256*1024*1024 {
			return errors.New("component archive is too large")
		}

		destination := filepath.Join(root, clean)
		if item.FileInfo().IsDir() {
			if err := os.MkdirAll(destination, 0700); err != nil {
				return err
			}

			continue
		}

		if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
			return err
		}

		source, err := item.Open()
		if err != nil {
			return err
		}

		target, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			source.Close()

			return err
		}

		written, copyErr := io.Copy(target, io.LimitReader(source, int64(item.UncompressedSize64)+1))
		sourceErr := source.Close()
		targetErr := target.Close()
		if copyErr != nil {
			return copyErr
		}

		if sourceErr != nil {
			return sourceErr
		}

		if targetErr != nil {
			return targetErr
		}

		if uint64(written) != item.UncompressedSize64 {
			return fmt.Errorf("incomplete component %q", name)
		}
	}

	for _, asset := range assetNames {
		name := "app/" + asset
		if !seen[strings.ToLower(filepath.FromSlash(name))] {
			return fmt.Errorf("missing component %s", name)
		}
	}

	return nil
}
