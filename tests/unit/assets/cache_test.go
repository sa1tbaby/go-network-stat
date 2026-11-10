package assets_test

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/sa1tbaby/go-network-stat/backend/api/assets"
)

func fixturePayload(t *testing.T, additions map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	files := map[string]string{
		"app/dashboard.html":        "test panel",
		"app/official-relays.json":  "{}",
		"app/cod-test-targets.json": "{}",
	}
	for key, value := range additions {
		files[key] = value
	}

	for name, value := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := entry.Write([]byte(value)); err != nil {
			t.Fatal(err)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	return buffer.Bytes()
}

func TestPayloadExtractionAndReusePreservesMutableSettings(t *testing.T) {
	cache := t.TempDir()
	archive := fixturePayload(t, nil)
	first, err := assets.PrepareArchive(cache, archive)
	if err != nil {
		t.Fatal(err)
	}

	settings := filepath.Join(first.Root, "log-settings.json")
	if err := os.WriteFile(settings, []byte("chosen directory"), 0600); err != nil {
		t.Fatal(err)
	}

	second, err := assets.PrepareArchive(cache, archive)
	if err != nil || first != second {
		t.Fatalf("cache not reused: %v / %v", second, err)
	}

	got, err := os.ReadFile(settings)
	if err != nil || string(got) != "chosen directory" {
		t.Fatalf("user settings changed: %s / %v", got, err)
	}

	entries, _ := os.ReadDir(cache)
	if len(entries) != 1 {
		t.Fatalf("temporary extraction directory left behind: %d", len(entries))
	}
}

func TestPayloadRejectsTraversalAndUnknownGroups(t *testing.T) {
	for _, name := range []string{"app/../../outside.txt", "../outside.txt", "/absolute.txt", "app/C:drive", "app/back\\slash", "unknown/file.txt"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if _, err := assets.PrepareArchive(root, fixturePayload(t, map[string]string{name: "bad"})); err == nil {
				t.Fatal("unsafe entry was accepted")
			}
		})
	}
}

func TestPayloadFailureDoesNotPublishCache(t *testing.T) {
	cache := t.TempDir()
	_, err := assets.PrepareArchive(cache, []byte("not a zip"))
	if err == nil {
		t.Fatal("bad archive accepted")
	}

	entries, readErr := os.ReadDir(cache)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("partial cache published: %v / %v", entries, readErr)
	}
}
