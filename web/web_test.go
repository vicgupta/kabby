package web

import (
	"strings"
	"testing"
)

func TestPageHTMLContainsApp(t *testing.T) {
	if !strings.Contains(pageHTML, "id=\"app\"") {
		t.Fatal("expected app div in page HTML")
	}
	if !strings.Contains(pageHTML, "/client.js") {
		t.Fatal("expected client.js script reference")
	}
}

func TestStylesCSSContainsBasics(t *testing.T) {
	if !strings.Contains(stylesCSS, "body") {
		t.Fatal("expected body style")
	}
	if !strings.Contains(stylesCSS, ".message") {
		t.Fatal("expected message style")
	}
}

func TestClientJSContainsConnect(t *testing.T) {
	if !strings.Contains(clientJS, "WebSocket") {
		t.Fatal("expected WebSocket in client JS")
	}
}

func TestStoreCreate(t *testing.T) {
	store := NewStore(t.TempDir())
	rec := store.Create("/tmp/test-workspace")
	if rec.ID == "" {
		t.Fatal("expected session ID")
	}
	if rec.Workspace != "/tmp/test-workspace" {
		t.Fatalf("expected workspace, got %s", rec.Workspace)
	}
	got, ok := store.Get(rec.ID)
	if !ok {
		t.Fatal("expected to get created session")
	}
	if got.ID != rec.ID {
		t.Fatalf("expected same ID, got %s", got.ID)
	}
}

func TestStoreList(t *testing.T) {
	store := NewStore(t.TempDir())
	store.Create("/tmp/w1")
	store.Create("/tmp/w2")
	if len(store.List()) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(store.List()))
	}
}
