package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"vibe-talk/internal/translation"

	"gopkg.in/yaml.v3"
)

// TranslationProfile is one named OpenAI-compatible endpoint + model.
// Stored in ~/.vibe-talk/config.yaml so restarts keep every provider.
type TranslationProfile struct {
	ID      string `yaml:"id"`
	Name    string `yaml:"name"`
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key,omitempty"`
	Model   string `yaml:"model"`
	Target  string `yaml:"target"`
}

// AppConfig is the persisted subset of app state.
type AppConfig struct {
	Language          string               `yaml:"language"`
	LowCPU            bool                 `yaml:"low_cpu"`
	IncludeMicrophone bool                 `yaml:"include_microphone"`
	NoiseFilter       bool                 `yaml:"noise_filter"`
	NoiseFloorDB      float64              `yaml:"noise_floor_db"`
	ClickThrough      bool                 `yaml:"click_through"`
	TranslateEnabled  bool                 `yaml:"translate_enabled"`
	ActiveProfileID   string               `yaml:"active_profile_id"`
	Profiles          []TranslationProfile `yaml:"profiles"`
}

var (
	configPathOverride string
	configSaveMu       sync.Mutex
)

// SetConfigPathOverride pins the config file location (used by tests).
func SetConfigPathOverride(path string) { configPathOverride = path }

// ConfigDir returns ~/.vibe-talk, creating nothing by itself.
func ConfigDir() (string, error) {
	if configPathOverride != "" {
		return filepath.Dir(configPathOverride), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", errors.New("cannot locate home directory for config")
	}
	return filepath.Join(home, ".vibe-talk"), nil
}

// ConfigPath returns the YAML file location.
// VIBE_TALK_CONFIG overrides it explicitly.
func ConfigPath() (string, error) {
	if configPathOverride != "" {
		return configPathOverride, nil
	}
	if env := strings.TrimSpace(os.Getenv("VIBE_TALK_CONFIG")); env != "" {
		return env, nil
	}
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

func defaultProfile() TranslationProfile {
	return TranslationProfile{
		ID:      "openai",
		Name:    "OpenAI",
		BaseURL: "https://api.openai.com/v1",
		Model:   "",
		Target:  "English",
	}
}

// DefaultConfig is used on first launch or when the file is unusable.
func DefaultConfig() AppConfig {
	p := defaultProfile()
	return AppConfig{
		NoiseFloorDB:    -45,
		ActiveProfileID: p.ID,
		Profiles:        []TranslationProfile{p},
	}
}

// LoadConfig reads the YAML file, falling back to defaults.
func LoadConfig() AppConfig {
	cfg := DefaultConfig()
	path, err := ConfigPath()
	if err != nil {
		return cfg
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	var file AppConfig
	if err := yaml.Unmarshal(data, &file); err != nil {
		return cfg
	}
	if len(file.Profiles) == 0 {
		file.Profiles = cfg.Profiles
	}
	if file.NoiseFloorDB == 0 {
		file.NoiseFloorDB = -45
	}
	// Normalize profile IDs and repair the active pointer.
	seen := map[string]bool{}
	for i := range file.Profiles {
		p := &file.Profiles[i]
		if strings.TrimSpace(p.ID) == "" {
			p.ID = fmt.Sprintf("profile-%d", i+1)
		}
		if strings.TrimSpace(p.Name) == "" {
			p.Name = p.ID
		}
		p.BaseURL = strings.TrimSpace(p.BaseURL)
		p.Model = strings.TrimSpace(p.Model)
		if strings.TrimSpace(p.Target) == "" {
			p.Target = "English"
		}
		if seen[p.ID] {
			p.ID = fmt.Sprintf("%s-%d", p.ID, i+1)
		}
		seen[p.ID] = true
	}
	if file.ActiveProfileID == "" || !seen[file.ActiveProfileID] {
		file.ActiveProfileID = file.Profiles[0].ID
	}
	return file
}

// SaveConfig writes cfg atomically with 0600 permissions (it holds API keys).
func SaveConfig(cfg AppConfig) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	configSaveMu.Lock()
	defer configSaveMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	header := []byte("# vibe-talk config — API keys are stored here with 0600 permissions.\n")
	tmp, err := os.CreateTemp(filepath.Dir(path), "config-*.yaml")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(append(header, data...)); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// Snapshot captures the persistable part of the running app.
func (a *app) snapshot() AppConfig {
	profiles := make([]TranslationProfile, len(a.profiles))
	copy(profiles, a.profiles)
	return AppConfig{
		Language:          a.language,
		LowCPU:            a.lowCPU,
		IncludeMicrophone: a.includeMicrophone,
		NoiseFilter:       a.noiseFilter,
		NoiseFloorDB:      a.noiseFloorDB,
		ClickThrough:      a.clickThrough,
		TranslateEnabled:  a.translateEnabled,
		ActiveProfileID:   a.activeProfileID,
		Profiles:          profiles,
	}
}

// persist saves settings without ever blocking transcription.
func (a *app) persist() {
	cfg := a.snapshot()
	go func() { _ = SaveConfig(cfg) }()
}

func (a *app) persistSync() { _ = SaveConfig(a.snapshot()) }

// activeProfile returns the selected profile and its index.
func (a *app) activeProfile() (TranslationProfile, int) {
	for i, p := range a.profiles {
		if p.ID == a.activeProfileID {
			return p, i
		}
	}
	if len(a.profiles) > 0 {
		return a.profiles[0], 0
	}
	p := defaultProfile()
	return p, -1
}

// activeTranslationConfig converts the active profile for the HTTP client.
func (a *app) activeTranslationConfig() translation.Config {
	p, _ := a.activeProfile()
	return translation.Config{
		BaseURL: p.BaseURL,
		APIKey:  p.APIKey,
		Model:   p.Model,
		Target:  p.Target,
	}
}

// applyConfig loads persisted state into a fresh app struct.
func (a *app) applyConfig(cfg AppConfig) {
	a.language = cfg.Language
	a.lowCPU = cfg.LowCPU
	a.includeMicrophone = cfg.IncludeMicrophone
	a.noiseFilter = cfg.NoiseFilter
	a.noiseFloorDB = cfg.NoiseFloorDB
	if a.noiseFloorDB == 0 {
		a.noiseFloorDB = -45
	}
	a.clickThrough = cfg.ClickThrough
	a.translateEnabled = cfg.TranslateEnabled
	a.profiles = cfg.Profiles
	a.activeProfileID = cfg.ActiveProfileID
	if len(a.profiles) == 0 {
		d := DefaultConfig()
		a.profiles, a.activeProfileID = d.Profiles, d.ActiveProfileID
		return
	}
	found := false
	for _, p := range a.profiles {
		if p.ID == a.activeProfileID {
			found = true
			break
		}
	}
	if !found {
		a.activeProfileID = a.profiles[0].ID
	}
}
