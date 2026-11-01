package fileutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sa1tbaby/go-network-stat/backend/infrastructure/fileutil"
)

func TestFailedReplacementKeepsPreviousSnapshot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "status.json")
	old := []byte(`{"complete":true}`)
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatal(err)
	}

	if err := fileutil.Replace(filepath.Join(dir, "missing.tmp"), path); err == nil {
		t.Fatal("missing source accepted")
	}

	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != string(old) {
		t.Fatalf("previous snapshot damaged: %s, %v", actual, err)
	}

	next := filepath.Join(dir, "next.tmp")
	if err := os.WriteFile(next, []byte(`{"next":true}`), 0600); err != nil {
		t.Fatal(err)
	}

	if err := fileutil.Replace(next, path); err != nil {
		t.Fatal(err)
	}

	actual, err = os.ReadFile(path)
	if err != nil || string(actual) != `{"next":true}` {
		t.Fatalf("snapshot not replaced: %s, %v", actual, err)
	}
}
