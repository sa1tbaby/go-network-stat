package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sa1tbaby/go-network-stat/backend/infrastructure/data"
)

func TestDedicatedDataTreeForPortableAndDevelopmentLaunches(t *testing.T) {
	root := t.TempDir()
	for _, executable := range []string{filepath.Join(root, "InternetStability.exe"), filepath.Join(root, "dist", "InternetStability.exe")} {
		if actual := data.DefaultRoot(executable); actual != filepath.Join(root, "data") {
			t.Fatalf("unexpected data directory for %s: %s", executable, actual)
		}
	}

	layout, err := data.Resolve(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}

	for _, dir := range []string{layout.Config, layout.Runtime, layout.Logs, layout.Statistics, layout.Cache, layout.Build, layout.UDP} {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() || filepath.Dir(dir) != layout.Root {
			t.Fatalf("service output escaped dedicated data tree: %s / %v", dir, err)
		}
	}

	if _, err := data.Resolve("relative-data"); err == nil {
		t.Fatal("relative explicit data directory accepted")
	}
}

func TestOutputSubdirectoryCannotRedirectOutsideData(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	layout, err := data.New(root)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(outside, layout.Cache); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := layout.Create(); err == nil {
		t.Fatal("symlink redirected service output outside data")
	}

	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("outside directory was modified: %v / %v", entries, err)
	}
}
