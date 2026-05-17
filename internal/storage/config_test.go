package storage

import (
	"os"
	"path/filepath"
	"testing"
)

// withConfigHome forces configPath() to read/write under a temp dir for the
// duration of the test by overriding HOME.
func withConfigHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	// Some platforms read XDG_CONFIG_HOME; keep them out of the picture so
	// the test only exercises ~/.config/claudetui under our temp HOME.
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
}

func TestConfig_LoadMissingReturnsZero(t *testing.T) {
	withConfigHome(t)
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig on missing file: %v", err)
	}
	if (cfg != Config{}) {
		t.Errorf("missing config should be zero, got %+v", cfg)
	}
}

func TestConfig_SaveLoadRoundtrip(t *testing.T) {
	withConfigHome(t)

	want := Config{Theme: "dracula", Model: "opus", Vim: true}
	if err := SaveConfig(want); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	got, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got != want {
		t.Errorf("roundtrip mismatch: got %+v want %+v", got, want)
	}
}

func TestConfig_LoadCorruptReturnsErr(t *testing.T) {
	withConfigHome(t)
	p, err := configPath()
	if err != nil {
		t.Fatalf("configPath: %v", err)
	}
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadConfig(); err == nil {
		t.Errorf("LoadConfig should error on corrupt file")
	}
}
