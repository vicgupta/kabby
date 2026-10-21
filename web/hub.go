package web

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
)

// Hub is the SSE web hub serving multiple workspaces and sessions.
type Hub struct {
	store     *Store
	port      int
	mu        sync.Mutex
	listeners map[string]chan []byte // sessionID -> broadcast channel
}

// NewHub creates a Hub.
func NewHub(store *Store, port int) *Hub {
	return &Hub{
		store:     store,
		port:      port,
		listeners: make(map[string]chan []byte),
	}
}

// Start starts the HTTP + SSE server and blocks.
func (h *Hub) Start() error {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/sessions", h.handleSessions)
	mux.HandleFunc("/api/sessions/", h.handleSession)
	mux.HandleFunc("/api/stream/", h.handleStream)
	mux.HandleFunc("/", h.handlePage)

	addr := fmt.Sprintf("127.0.0.1:%d", h.port)
	log.Printf("kabby web hub listening on http://%s\n", addr)
	return http.ListenAndServe(addr, mux)
}

func (h *Hub) handlePage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(pageHTML))
}

func (h *Hub) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		sessions := h.store.List()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sessions)
	case http.MethodPost:
		rec := h.store.Create(r.FormValue("workspace"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rec)
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (h *Hub) handleSession(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	switch r.Method {
	case http.MethodGet:
		rec, ok := h.store.Get(id)
		if !ok {
			http.Error(w, "not found", 404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rec)
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (h *Hub) handleStream(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/stream/")
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}

	ch := make(chan []byte, 16)
	h.mu.Lock()
	h.listeners[id] = ch
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.listeners, id)
		h.mu.Unlock()
		close(ch)
	}()

	// Initial connection established message.
	fmt.Fprintf(w, "event: connected\ndata: {\"session\":\"%s\"}\n\n", id)
	flusher.Flush()

	for msg := range ch {
		fmt.Fprintf(w, "data: %s\n\n", string(msg))
		flusher.Flush()
	}
}

// broadcast sends a message to a session's stream listeners.
func (h *Hub) broadcast(sessionID string, msg []byte) {
	h.mu.Lock()
	ch, ok := h.listeners[sessionID]
	h.mu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- msg:
	default:
	}
}
