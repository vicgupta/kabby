package web

import (
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SessionRecord is a persisted chat session.
type SessionRecord struct {
	ID        string    `json:"id"`
	Workspace string    `json:"workspace"`
	Title     string    `json:"title"`
	Created   time.Time `json:"created"`
	Updated   time.Time `json:"updated"`
	Messages  []Message `json:"messages"`
}

// Message is a single chat message.
type Message struct {
	ID        string     `json:"id"`
	Role      string     `json:"role"` // "user", "assistant", "tool", "system"
	Content   string     `json:"content"`
	Created   time.Time  `json:"created"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall in a message.
type ToolCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// Store persists sessions under ~/.kabby/sessions/.
type Store struct {
	mu       sync.Mutex
	dir      string
	sessions map[string]*SessionRecord
}

// NewStore creates a Store backed by the given directory.
func NewStore(dir string) *Store {
	return &Store{
		dir:      dir,
		sessions: make(map[string]*SessionRecord),
	}
}

// Open loads existing sessions from disk.
func (s *Store) Open() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var rec SessionRecord
		if err := json.Unmarshal(data, &rec); err != nil {
			continue
		}
		s.sessions[rec.ID] = &rec
	}
	return nil
}

// Save persists a session to disk.
func (s *Store) Save(rec *SessionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec.Updated = time.Now()
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(s.dir, rec.ID+".json")
	return os.WriteFile(path, data, 0o600)
}

// Get returns a session by ID.
func (s *Store) Get(id string) (*SessionRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.sessions[id]
	return rec, ok
}

// List returns all sessions.
func (s *Store) List() []*SessionRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*SessionRecord, 0, len(s.sessions))
	for _, rec := range s.sessions {
		out = append(out, rec)
	}
	return out
}

// Create creates a new session.
func (s *Store) Create(workspace string) *SessionRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := generateID()
	rec := &SessionRecord{
		ID:        id,
		Workspace: workspace,
		Title:     "",
		Created:   time.Now(),
		Updated:   time.Now(),
	}
	s.sessions[id] = rec
	go s.Save(rec)
	return rec
}

func generateID() string {
	ts := time.Now().Format("20060102-150405")
	b := make([]byte, 6)
	rand.Read(b)
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	var suffix string
	for _, v := range b {
		suffix += string(chars[int(v)%len(chars)])
	}
	return ts + "-" + suffix
}
