package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sa1tbaby/go-network-stat/backend/config"
	"github.com/sa1tbaby/go-network-stat/backend/infrastructure/data"
	"github.com/sa1tbaby/go-network-stat/backend/infrastructure/fileutil"
)

func TestPortableConfigRoundTrip(t *testing.T) {
	layout, err := data.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if err := layout.Create(); err != nil {
		t.Fatal(err)
	}

	dir := layout.Config
	cfg, err := config.Load(layout, nil)
	if err != nil {
		t.Fatal(err)
	}

	cfg.LogDirectory = filepath.Join(dir, "Logs — folder with spaces")
	cfg.StartPaused = true
	cfg.IntervalMs = 250
	for i := 0; i < 2; i++ {
		if err := config.Save(layout, cfg); err != nil {
			t.Fatal(err)
		}
	}

	actual, err := config.Load(layout, nil)
	if err != nil {
		t.Fatal(err)
	}

	if actual != cfg {
		t.Fatalf("settings changed: %#v != %#v", actual, cfg)
	}
}

func TestRecoversBrowserFolderAfterAbruptClientExit(t *testing.T) {
	layout, err := data.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if err := layout.Create(); err != nil {
		t.Fatal(err)
	}

	dir := layout.Config
	cfg, err := config.Load(layout, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := config.Save(layout, cfg); err != nil {
		t.Fatal(err)
	}

	settingsPath := filepath.Join(dir, "settings.json")
	past := time.Now().Add(-time.Minute)
	if err := os.Chtimes(settingsPath, past, past); err != nil {
		t.Fatal(err)
	}

	selected := filepath.Join(dir, "selected-in-browser")
	if err := fileutil.WriteJSON(filepath.Join(dir, "log-settings.json"), map[string]string{"appId": "codex-internet-stability", "directory": selected}); err != nil {
		t.Fatal(err)
	}

	actual, err := config.Load(layout, nil)
	if err != nil {
		t.Fatal(err)
	}

	if actual.LogDirectory != selected {
		t.Fatal("browser selection was lost after abrupt exit")
	}
}

func TestInvalidConfigDoesNotOverwriteExistingFile(t *testing.T) {
	layout, err := data.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if err := layout.Create(); err != nil {
		t.Fatal(err)
	}

	dir := layout.Config
	path := filepath.Join(dir, "settings.json")
	original := []byte(`{"logDirectory":"relative\\logs"}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := config.Load(layout, nil); err == nil {
		t.Fatal("relative directory accepted")
	}

	actual, _ := os.ReadFile(path)
	if string(actual) != string(original) {
		t.Fatal("existing settings were modified")
	}
}

func TestImportsFlatConfigPreservingLegacyHistory(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "old")
	if err := os.MkdirAll(old, 0700); err != nil {
		t.Fatal(err)
	}

	history := filepath.Join(old, "history.csv")
	if err := os.WriteFile(history, []byte("old history"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := fileutil.WriteJSON(filepath.Join(old, "settings.json"), config.Config{
		LogDirectory: old,
		IntervalMs:   250,
		Port:         18865,
	}); err != nil {
		t.Fatal(err)
	}

	if err := fileutil.WriteJSON(filepath.Join(old, "custom-targets.json"), []string{"1.1.1.1"}); err != nil {
		t.Fatal(err)
	}

	layout, err := data.New(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(layout, []string{old})
	if err != nil {
		t.Fatal(err)
	}

	if cfg.LogDirectory != old || cfg.IntervalMs != 250 || cfg.RuntimeDirectory != layout.Runtime || cfg.StateDirectory != layout.Config {
		t.Fatalf("lost legacy preferences: %+v", cfg)
	}

	if actual, err := os.ReadFile(history); err != nil || string(actual) != "old history" {
		t.Fatal("legacy history altered", err)
	}

	if _, err := os.Stat(filepath.Join(layout.Config, "custom-targets.json")); err != nil {
		t.Fatal("targets not imported", err)
	}

	cfg.StartPaused = true
	if err := config.Save(layout, cfg); err != nil {
		t.Fatal(err)
	}

	reloaded, err := config.Load(layout, []string{old})
	if err != nil || !reloaded.StartPaused {
		t.Fatal("import overwrote newer config", err)
	}
}

func TestLegacyImportPreservesBrowserAndDesktopPreferenceOrder(t *testing.T) {
	for _, browserNewer := range []bool{false, true} {
		t.Run(map[bool]string{false: "desktop_newer", true: "browser_newer"}[browserNewer], func(t *testing.T) {
			root := t.TempDir()
			old := filepath.Join(root, "old")
			if err := os.MkdirAll(old, 0700); err != nil {
				t.Fatal(err)
			}

			desktopDirectory, browserDirectory := filepath.Join(root, "desktop-history"), filepath.Join(root, "browser-history")
			settingsPath, backendPath := filepath.Join(old, "settings.json"), filepath.Join(old, "log-settings.json")
			if err := fileutil.WriteJSON(settingsPath, config.Config{
				LogDirectory: desktopDirectory,
				IntervalMs:   1000,
				Port:         8765,
			}); err != nil {
				t.Fatal(err)
			}

			if err := fileutil.WriteJSON(backendPath, map[string]string{"appId": "codex-internet-stability", "directory": browserDirectory}); err != nil {
				t.Fatal(err)
			}

			older, newer := time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour)
			desktopTime, browserTime := newer, older
			wanted := desktopDirectory
			if browserNewer {
				desktopTime, browserTime, wanted = older, newer, browserDirectory
			}

			if err := os.Chtimes(settingsPath, desktopTime, desktopTime); err != nil {
				t.Fatal(err)
			}

			if err := os.Chtimes(backendPath, browserTime, browserTime); err != nil {
				t.Fatal(err)
			}

			layout, err := data.New(filepath.Join(root, "data"))
			if err != nil {
				t.Fatal(err)
			}

			cfg, err := config.Load(layout, []string{old})
			if err != nil {
				t.Fatal(err)
			}

			if cfg.LogDirectory != wanted {
				t.Fatalf("migration changed preference order: got %s, want %s", cfg.LogDirectory, wanted)
			}
		})
	}
}
