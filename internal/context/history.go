package context

// Transcript is the harness-held conversation history, organized into groups.
// Groups can be removed (eviction) and write bodies are kept as marked records
// rather than re-imagined assistant text.
type Transcript struct {
	Groups []HistoryGroup
}

// NewTranscript creates an empty transcript.
func NewTranscript() *Transcript {
	return &Transcript{}
}

// AddGroup appends a group to the transcript.
func (t *Transcript) AddGroup(g HistoryGroup) {
	t.Groups = append(t.Groups, g)
}

// RemoveGroup removes the group with the given ID.
func (t *Transcript) RemoveGroup(id string) bool {
	for i, g := range t.Groups {
		if g.ID == id {
			t.Groups = append(t.Groups[:i], t.Groups[i+1:]...)
			return true
		}
	}
	return false
}

// TotalTokens returns the sum of tokens across all groups.
func (t *Transcript) TotalTokens() int {
	total := 0
	for _, g := range t.Groups {
		total += g.Tokens
	}
	return total
}

// MarkWriteBody marks a group as a harness-applied write-body record
// (not model-generated text).
func (t *Transcript) MarkWriteBody(id string) bool {
	for i := range t.Groups {
		if t.Groups[i].ID == id {
			t.Groups[i].Evictable = true // write bodies are evictable
			return true
		}
	}
	return false
}

// Find returns the group with the given ID, or nil.
func (t *Transcript) Find(id string) *HistoryGroup {
	for i := range t.Groups {
		if t.Groups[i].ID == id {
			return &t.Groups[i]
		}
	}
	return nil
}
