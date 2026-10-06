package translation

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestModelsAndTranslation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing authorization")
		}
		switch r.URL.Path {
		case "/v1/models":
			w.Write([]byte(`{"data":[{"id":"z"},{"id":"a"},{"id":"a"}]}`))
		case "/v1/chat/completions":
			var body struct {
				Model    string
				Messages []map[string]string
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Model != "a" || len(body.Messages) != 2 || body.Messages[1]["content"] != "Hello" {
				t.Errorf("bad payload: %+v", body)
			}
			w.Write([]byte(`{"choices":[{"message":{"content":" Bonjour "}}]}`))
		default:
			t.Error(r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	cfg := Config{BaseURL: server.URL + "/v1/", APIKey: "secret", Model: "a", Target: "French"}
	client := NewClient()
	models, err := client.Models(context.Background(), cfg)
	if err != nil || len(models) != 2 || models[0] != "a" {
		t.Fatalf("%v %v", models, err)
	}
	result, err := client.Translate(context.Background(), cfg, "Hello")
	if err != nil || result != "Bonjour" {
		t.Fatalf("%q %v", result, err)
	}
}
func TestValidationAndFailures(t *testing.T) {
	for _, endpoint := range []string{"file:///tmp/key", "http://remote.example/v1", "https://user:pass@example.com/v1", "https://example.com/v1?key=secret"} {
		if err := (Config{BaseURL: endpoint, Model: "m", Target: "French"}).Validate(); err == nil {
			t.Errorf("accepted %s", endpoint)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte("sensitive provider response"))
	}))
	defer server.Close()
	_, err := NewClient().Translate(context.Background(), Config{BaseURL: server.URL, Model: "m", Target: "French"}, "text")
	if err == nil || err.Error() != "Translation API returned HTTP 401; check endpoint, key and model" {
		t.Fatal(err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestModelDiscoveryTimeout(t *testing.T) {
	client := NewClient()
	client.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		remaining := time.Until(deadline)
		if !ok || remaining < 59*time.Second || remaining > 60*time.Second {
			t.Errorf("model deadline: %v", remaining)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[]}`))}, nil
	})
	if _, err := client.Models(context.Background(), Config{BaseURL: "http://localhost/v1"}); err != nil {
		t.Fatal(err)
	}
	if client.HTTP.Timeout != 20*time.Second {
		t.Fatal("discovery changed live translation timeout")
	}
}

func TestTimeoutMessage(t *testing.T) {
	client := NewClient()
	client.HTTP.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, context.DeadlineExceeded })
	_, err := client.Models(context.Background(), Config{BaseURL: "http://localhost/v1"})
	if err == nil || !strings.Contains(err.Error(), "models request timed out after 1m0s") {
		t.Fatal(err)
	}
}

func TestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewClient().Models(ctx, Config{BaseURL: "http://localhost:1/v1"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}
