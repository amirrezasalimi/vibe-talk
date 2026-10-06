//go:build darwin && arm64

package transcription

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWhistleNative(t *testing.T) {
	if os.Getenv("VIBE_TALK_NATIVE_TEST") != "1" {
		t.Skip("set VIBE_TALK_NATIVE_TEST=1 to load native model")
	}
	// Go runs package tests from this directory, not the repository root.
	if os.Getenv("VIBE_TALK_RUNTIME") == "" {
		dir, err := filepath.Abs("../../runtime")
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("VIBE_TALK_RUNTIME", dir)
	}
	speechSessionMutex.Lock()
	defer speechSessionMutex.Unlock()
	engine, err := newSpeechEngine()
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		event, err := engine.Process(make([]float32, 16000), "en")
		if err != nil {
			t.Fatal(err)
		}
		if event.Type != "transcript" || event.Text != "" {
			t.Fatalf("silence result: %+v", event)
		}
		tail, err := engine.Flush()
		if err != nil || tail.Type != "transcript" {
			t.Fatalf("flush: %+v %v", tail, err)
		}
	}
}
