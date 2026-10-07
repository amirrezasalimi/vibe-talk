package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "vibe-talk-test")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	SetConfigPathOverride(filepath.Join(dir, "config.yaml"))
	os.Exit(m.Run())
}

func TestConfigRoundTrip(t *testing.T) {
	path, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Language = "fr"
	cfg.LowCPU = true
	cfg.NoiseFloorDB = -40
	cfg.Profiles = append(cfg.Profiles, TranslationProfile{
		ID: "local", Name: "Local", BaseURL: "http://localhost:11434/v1",
		Model: "llama", Target: "German", APIKey: "secret",
	})
	cfg.ActiveProfileID = "local"
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config perms = %o, want 600", info.Mode().Perm())
	}
	loaded := LoadConfig()
	if loaded.Language != "fr" || !loaded.LowCPU || len(loaded.Profiles) != 2 {
		t.Fatalf("round trip lost data: %+v", loaded)
	}
	if loaded.ActiveProfileID != "local" || loaded.Profiles[1].APIKey != "secret" {
		t.Fatalf("active/api key lost: %+v", loaded)
	}
}

func TestConfigDefaultsOnMissing(t *testing.T) {
	prev, _ := ConfigPath()
	missing := filepath.Join(t.TempDir(), "config.yaml")
	SetConfigPathOverride(missing)
	defer SetConfigPathOverride(prev)
	cfg := LoadConfig()
	if len(cfg.Profiles) == 0 || cfg.NoiseFloorDB != -45 {
		t.Fatalf("bad defaults: %+v", cfg)
	}
}

func TestApplyConfigRepairsActive(t *testing.T) {
	a := &app{}
	a.applyConfig(AppConfig{Profiles: []TranslationProfile{
		{ID: "a", Name: "A", BaseURL: "https://api.openai.com/v1"},
	}, ActiveProfileID: "gone"})
	if a.activeProfileID != "a" {
		t.Fatalf("active not repaired: %+v", a)
	}
	if cfg := a.activeTranslationConfig(); cfg.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("conversion failed: %+v", cfg)
	}
}
