package app

import (
	"github.com/egoist/mygo/ui"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"vibe-talk/internal/translation"
)

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
		translationConfig: translation.Config{BaseURL: server.URL, Model: "test", Target: "French"},
		dispatch:          func(f func()) { callbacks <- f }}
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
	a := &app{translationConfig: translation.Config{BaseURL: "https://api.openai.com/v1", Target: "French"}, models: []string{"fast-model", "other-model"}}
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
