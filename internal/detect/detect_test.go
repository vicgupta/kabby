package detect

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProbeIdentifiesOllama(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			fmt.Fprint(w, `{"models":[{"name":"llama3"}]}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	result := Probe(context.Background(), srv.URL)
	if result.Backend != "ollama" {
		t.Fatalf("expected ollama, got %s", result.Backend)
	}
	if result.Error != nil {
		t.Fatalf("expected no error, got %v", result.Error)
	}
}

func TestProbeIdentifiesOllamaRegardlessOfPort(t *testing.T) {
	// Use a non-standard port to prove identification is by response.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			fmt.Fprint(w, `{"models":[{"name":"llama3"}]}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	result := Probe(context.Background(), srv.URL)
	if result.Backend != "ollama" {
		t.Fatalf("expected ollama regardless of port, got %s", result.Backend)
	}
}

func TestProbeIdentifiesLMStudio(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/models" {
			fmt.Fprint(w, `{"data":[{"id":"qwen2.5"}]}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	result := Probe(context.Background(), srv.URL)
	if result.Backend != "lmstudio" {
		t.Fatalf("expected lmstudio, got %s", result.Backend)
	}
}

func TestProbeIdentifiesOMLX(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			fmt.Fprint(w, `{"engine_pool":{}}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	result := Probe(context.Background(), srv.URL)
	if result.Backend != "omlx" {
		t.Fatalf("expected omlx, got %s", result.Backend)
	}
}

func TestProbeIdentifiesMTPLX(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			fmt.Fprint(w, `{"mtp_enabled":true,"generation_mode":"chat"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	result := Probe(context.Background(), srv.URL)
	if result.Backend != "mtplx" {
		t.Fatalf("expected mtplx, got %s", result.Backend)
	}
}

func TestProbeDeadAddress(t *testing.T) {
	// Use a port that's almost certainly not listening.
	result := Probe(context.Background(), "http://127.0.0.1:19999")
	if result.Backend != "" {
		t.Fatalf("expected no backend for dead address, got %s", result.Backend)
	}
	if result.Error == nil {
		t.Fatal("expected error for dead address")
	}
}

func TestDiscoverLocalConcurrent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			fmt.Fprint(w, `{"models":[{"name":"llama3"}]}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	// Probe the server's URL directly.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := Probe(ctx, srv.URL)
	if result.Backend != "ollama" {
		t.Fatalf("expected ollama, got %s", result.Backend)
	}
}

func TestSavedHostProbeURLsBare(t *testing.T) {
	host := SavedHost{Address: "example.com"}
	urls := host.ProbeURLs()
	if len(urls) != 2 {
		t.Fatalf("expected 2 URLs for bare host, got %d: %v", len(urls), urls)
	}
	if urls[0] != "http://example.com:11434" || urls[1] != "http://example.com:1234" {
		t.Fatalf("unexpected URLs: %v", urls)
	}
}

func TestSavedHostProbeURLsHostPort(t *testing.T) {
	host := SavedHost{Address: "example.com:5555"}
	urls := host.ProbeURLs()
	if len(urls) != 1 {
		t.Fatalf("expected 1 URL for host:port, got %d: %v", len(urls), urls)
	}
	if urls[0] != "http://example.com:5555" {
		t.Fatalf("unexpected URL: %s", urls[0])
	}
}

func TestSavedHostProbeURLsFullURL(t *testing.T) {
	host := SavedHost{Address: "http://example.com:7433"}
	urls := host.ProbeURLs()
	if len(urls) != 1 {
		t.Fatalf("expected 1 URL for full URL, got %d: %v", len(urls), urls)
	}
	if urls[0] != "http://example.com:7433" {
		t.Fatalf("unexpected URL: %s", urls[0])
	}
}

func TestSavedHostValidate(t *testing.T) {
	if err := (&SavedHost{Address: ""}).Validate(); err == nil {
		t.Fatal("expected validation error for empty address")
	}
	if err := (&SavedHost{Address: "example.com"}).Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if err := (&SavedHost{Address: "http://example.com:7433"}).Validate(); err != nil {
		t.Fatalf("unexpected validation error for URL: %v", err)
	}
}
