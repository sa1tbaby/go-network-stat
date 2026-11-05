package storage_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sa1tbaby/go-network-stat/backend/networkmanager/storage"
)

func TestAppendAndResetPreserveLegacyHeaders(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	defer store.Close()
	if err = store.AppendProbe([]string{"fixture"}); err != nil {
		t.Fatal(err)
	}

	if err = store.AppendEvent(map[string]string{
		"time":    "fixture",
		"kind":    "mark",
		"message": "example",
	}); err != nil {
		t.Fatal(err)
	}

	if err = store.Reset(); err != nil {
		t.Fatal(err)
	}

	for name, expected := range map[string]string{
		"probes.csv":   storage.ProbeHeader + "\n",
		"adapters.csv": storage.AdapterHeader + "\n",
		"events.jsonl": "",
	} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(data) != expected {
			t.Fatalf("%s content changed: %q %v", name, data, err)
		}
	}
}

func TestOpenRejectsForeignFilesBeforeAppending(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "probes.csv")
	if err := os.WriteFile(path, []byte("foreign\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := storage.Open(dir); err == nil {
		t.Fatal("foreign CSV was accepted")
	}

	data, _ := os.ReadFile(path)
	if string(data) != "foreign\n" {
		t.Fatal("foreign data modified")
	}
}

func TestAtomicSnapshotsAndBOMIdentity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	for _, value := range []string{"first", "second"} {
		if err := storage.PublishJSON(path, map[string]string{"value": value}); err != nil {
			t.Fatal(err)
		}
	}

	if !storage.JSONHas(path, "value", "second") {
		t.Fatal("atomic replacement did not publish completed value")
	}

	if err := os.WriteFile(path, []byte("\ufeff{\"appId\":\"owned\"}"), 0600); err != nil {
		t.Fatal(err)
	}

	if !storage.JSONHas(path, "appId", "owned") {
		t.Fatal("BOM legacy JSON not accepted")
	}

	if _, err := storage.FullPath("relative"); err == nil {
		t.Fatal("relative data path accepted")
	}

	if _, err := storage.FullPath(filepath.Join(dir, "bad\npath")); err == nil {
		t.Fatal("newline data path accepted")
	}

	if strings.Contains(string(storage.WithoutBOM([]byte("\ufeffok"))), "\ufeff") {
		t.Fatal("BOM not stripped")
	}
}
