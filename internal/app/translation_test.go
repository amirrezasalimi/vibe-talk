package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"vibe-talk/internal/translation"

	"github.com/egoist/mygo/ui"
)

func testProfile(baseURL, model string) []TranslationProfile {
	return []TranslationProfile{{
		ID: "test", Name: "Test", BaseURL: baseURL, Model: model, Target: "French",
	}}
}

func TestTranslationRowsAndClear(t *testing.T) {
	started, release := make(chan struct{}, 1), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-release
		w.Write([]byte(`{"choices":[{"message":{"content":"Bonjour"}}]}`))
	}))
	defer server.Close()
	callbacks := make(chan func(), 4)
	a := &app{translator: translation.NewClient(), translateEnabled: true,
		profiles: testProfile(server.URL, "test"), activeProfileID: "test",
		dispatch: func(f func()) { callbacks <- f }}
	a.appendCaption("Hello")
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("request never started")
	}
	if !a.lines[0].Translating {
		t.Fatal("missing pending state")
	}
	a.clear()
	a.translateEnabled = false
	a.appendCaption("New row")
	close(release)
	select {
	case f := <-callbacks:
		f()
	case <-time.After(3 * time.Second):
		t.Fatal("missing callback")
	}
	if len(a.lines) != 1 || a.lines[0].Translation != "" || a.lines[0].Translating {
		t.Fatalf("stale response attached: %+v", a.lines)
	}
	a.cancelTranslation()
}

func TestCompactTranslationSettings(t *testing.T) {
	a := &app{
		profiles:        testProfile("https://api.openai.com/v1", ""),
		activeProfileID: "test",
		modelsByProfile: map[string][]string{"test": {"fast-model", "other-model"}},
	}
	tt := ui.NewTester(a.view, 720, 620)
	if tt.HasText("API base URL (include /v1)") {
		t.Fatal("settings should start collapsed")
	}
	if err := tt.Click("Translation settings"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Model · search or enter ID") {
		t.Fatal(tt.Texts())
	}
	if err := tt.Click("Auto-translate new lines"); err != nil {
		t.Fatal(err)
	}
	if a.translateEnabled || a.translationError == "" {
		t.Fatal("invalid configuration accepted")
	}
}

func TestMultipleProfilesPersistActive(t *testing.T) {
	a := &app{
		profiles: []TranslationProfile{
			{ID: "a", Name: "Alpha", BaseURL: "https://api.openai.com/v1", Model: "m1", Target: "French"},
			{ID: "b", Name: "Beta", BaseURL: "http://localhost:11434/v1", Model: "m2", Target: "German"},
		},
		activeProfileID: "a",
	}
	if cfg := a.activeTranslationConfig(); cfg.Model != "m1" || cfg.Target != "French" {
		t.Fatalf("active profile wrong: %+v", cfg)
	}
	a.activeProfileID = "b"
	if cfg := a.activeTranslationConfig(); cfg.BaseURL != "http://localhost:11434/v1" {
		t.Fatalf("switch failed: %+v", cfg)
	}
	snap := a.snapshot()
	if len(snap.Profiles) != 2 || snap.ActiveProfileID != "b" {
		t.Fatalf("snapshot lost profiles: %+v", snap)
	}
}
