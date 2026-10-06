package transcription

import (
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSourceLanguage(t *testing.T) {
	for _, value := range []string{"", "auto", "en", "de", "fr", "es", "it", "nl", "pl", " EN "} {
		got, err := NormalizeLanguage(value)
		want := strings.ToLower(strings.TrimSpace(value))
		if want == "auto" {
			want = ""
		}
		if err != nil || got != want {
			t.Fatalf("%q: %q %v", value, got, err)
		}
	}
	if _, err := NormalizeLanguage("ru"); err == nil {
		t.Fatal("accepted unsupported language")
	}
}

type testCapture struct {
	remaining int
	closed    chan struct{}
	once      sync.Once
	fail      error
}

func (c *testCapture) Read(out []float32) (int, error) {
	if c.remaining > 0 {
		n := min(len(out), c.remaining, 333)
		c.remaining -= n
		return n, nil
	}
	if c.closed != nil {
		<-c.closed
	}
	if c.fail != nil {
		return 0, c.fail
	}
	return 0, io.EOF
}
func (c *testCapture) Close() error {
	if c.closed != nil {
		c.once.Do(func() { close(c.closed) })
	}
	return nil
}

type testEngine struct {
	sizes   []int
	flushed bool
}

func (e *testEngine) Process(p []float32, language string) (Event, error) {
	e.sizes = append(e.sizes, len(p))
	return Event{Type: "transcript", Text: "hello"}, nil
}
func (e *testEngine) Flush() (Event, error) {
	e.flushed = true
	return Event{Type: "transcript", Text: "tail"}, nil
}
func TestNativeFramingAndFlush(t *testing.T) {
	e := &testEngine{}
	c := &testCapture{remaining: 16500}
	var events []Event
	err := runNativeSession(&nativeSession{}, "en", func() (speechEngine, error) { return e, nil }, func() (audioCapture, error) { return c, nil }, func(ev Event) { events = append(events, ev) })
	if err != nil || !e.flushed || len(e.sizes) != 2 || e.sizes[0] != 16000 || e.sizes[1] != 500 {
		t.Fatalf("%+v %v", e, err)
	}
	if events[len(events)-1].Type != "done" || events[len(events)-2].Text != "tail" {
		t.Fatal(events)
	}
}
func TestCaptureErrorResetsEngine(t *testing.T) {
	e := &testEngine{}
	failure := errors.New("capture failed")
	err := runNativeSession(&nativeSession{}, "", func() (speechEngine, error) { return e, nil }, func() (audioCapture, error) { return &testCapture{fail: failure}, nil }, func(Event) {})
	if !errors.Is(err, failure) || !e.flushed {
		t.Fatalf("%v flushed=%v", err, e.flushed)
	}
}
func TestWorkerStop(t *testing.T) {
	c := &testCapture{closed: make(chan struct{})}
	e := &testEngine{}
	ready := make(chan struct{})
	done := make(chan error, 1)
	w := &Worker{engineFactory: func() (speechEngine, error) { return e, nil }, captureFactory: func() (audioCapture, error) { return c, nil }}
	if err := w.Start("", func(ev Event) {
		if strings.Contains(ev.Message, "Listening") {
			close(ready)
		}
	}, func(err error) { done <- err }); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("start timed out")
	}
	if err := w.Start("", func(Event) {}, func(error) {}); err == nil {
		t.Fatal("overlapping session accepted")
	}
	w.Stop()
	w.Stop()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("stop timed out")
	}
	if !e.flushed {
		t.Fatal("tail not flushed")
	}
}
