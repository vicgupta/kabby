package context

import (
	"testing"
)

func TestComputeBudget4k(t *testing.T) {
	b := ComputeBudget(4096)
	// reply_reserve = clamp(floor(4096/4)=1024, 128, 8192) = 1024
	if b.ReplyReserve != 1024 {
		t.Fatalf("expected reply_reserve 1024, got %d", b.ReplyReserve)
	}
	// safety = min(256, floor(4096*0.05)=204) = 204
	if b.Safety != 204 {
		t.Fatalf("expected safety 204, got %d", b.Safety)
	}
	// input = 4096 - 1024 - 204 = 2868
	if b.InputBudget != 2868 {
		t.Fatalf("expected input 2868, got %d", b.InputBudget)
	}
	if b.Window != 4096 {
		t.Fatalf("expected window 4096, got %d", b.Window)
	}
}

func TestComputeBudget16k(t *testing.T) {
	b := ComputeBudget(16384)
	// reply_reserve = clamp(4096, 128, 8192) = 4096
	if b.ReplyReserve != 4096 {
		t.Fatalf("expected reply_reserve 4096, got %d", b.ReplyReserve)
	}
	// safety = min(256, 819) = 256
	if b.Safety != 256 {
		t.Fatalf("expected safety 256, got %d", b.Safety)
	}
	// input = 16384 - 4096 - 256 = 12032
	if b.InputBudget != 12032 {
		t.Fatalf("expected input 12032, got %d", b.InputBudget)
	}
}

func TestComputeBudgetSmallWindow(t *testing.T) {
	b := ComputeBudget(1024)
	// reply_reserve = clamp(256, 128, 8192) = 256
	if b.ReplyReserve != 256 {
		t.Fatalf("expected reply_reserve 256, got %d", b.ReplyReserve)
	}
	// safety = min(256, 51) = 51
	if b.Safety != 51 {
		t.Fatalf("expected safety 51, got %d", b.Safety)
	}
	// input = 1024 - 256 - 51 = 717
	if b.InputBudget != 717 {
		t.Fatalf("expected input 717, got %d", b.InputBudget)
	}
}

func TestComputeBudgetTinyWindow(t *testing.T) {
	b := ComputeBudget(512) // below minimum, clamped to 1024
	if b.Window != 1024 {
		t.Fatalf("expected window clamped to 1024, got %d", b.Window)
	}
}

func TestInputUtilization(t *testing.T) {
	m := NewManager(4096) // input budget = 2868
	m.EstimateUsage(1434) // exactly 50%
	if util := m.InputUtilization(); util < 0.49 || util > 0.51 {
		t.Fatalf("expected ~0.5 utilization, got %f", util)
	}
}

func TestNeedsTier1Eviction(t *testing.T) {
	m := NewManager(4096) // threshold = 80% of 2868 = 2294
	m.EstimateUsage(2295)
	if !m.NeedsTier1Eviction() {
		t.Fatal("expected eviction needed at >80%")
	}
	m.EstimateUsage(2294)
	if m.NeedsTier1Eviction() {
		t.Fatal("expected no eviction at exactly 80%")
	}
}

func TestTier1Target(t *testing.T) {
	m := NewManager(4096) // target = 60% of 2868 = 1720
	if target := m.Tier1Target(); target != 1720 {
		t.Fatalf("expected tier1 target 1720, got %d", target)
	}
}

func TestEvictTier1ProtectsNewest(t *testing.T) {
	m := NewManager(4096) // input budget 2868, target 1720
	m.AddGroup(HistoryGroup{ID: "a", Tokens: 1000, Newest: false, Evictable: true})
	m.AddGroup(HistoryGroup{ID: "b", Tokens: 1000, Newest: false, Evictable: true})
	m.AddGroup(HistoryGroup{ID: "c", Tokens: 1000, Newest: true, Evictable: true}) // newest, protected
	// used = 3000, target = 1720, need to evict at least 1280
	evicted, err := m.EvictTier1()
	if err != nil {
		t.Fatal(err)
	}
	// Should evict 'a' and 'b' (1000+1000=2000 >= 1280 needed), but not 'c'
	evictedIDs := map[string]bool{}
	for _, g := range evicted {
		evictedIDs[g.ID] = true
	}
	if !evictedIDs["a"] || !evictedIDs["b"] {
		t.Fatalf("expected a and b evicted, got %v", evictedIDs)
	}
	if evictedIDs["c"] {
		t.Fatal("newest group c should not be evicted")
	}
	if m.used > 1720 {
		t.Fatalf("expected used <= target 1720, got %d", m.used)
	}
}

func TestEvictTier1NothingEvictable(t *testing.T) {
	m := NewManager(4096)
	m.AddGroup(HistoryGroup{ID: "a", Tokens: 5000, Newest: true, Evictable: false}) // newest, not evictable
	evicted, err := m.EvictTier1()
	if err != nil {
		t.Fatal(err)
	}
	if len(evicted) != 0 {
		t.Fatalf("expected nothing evicted, got %d", len(evicted))
	}
}

func TestCanFitInstructions(t *testing.T) {
	m := NewManager(4096) // input budget 2868
	if !m.CanFitInstructions(2000) {
		t.Fatal("expected 2000 to fit in 2868")
	}
	if m.CanFitInstructions(5000) {
		t.Fatal("expected 5000 not to fit in 2868")
	}
}

func TestImpossibleWindow(t *testing.T) {
	m := NewManager(1024) // input budget 717
	if err := m.ImpossibleWindow(1000); err == nil {
		t.Fatal("expected error for instructions exceeding input budget")
	}
	if err := m.ImpossibleWindow(500); err != nil {
		t.Fatalf("expected no error for 500 <= 717, got %v", err)
	}
}

func TestCompactionThreshold(t *testing.T) {
	m := NewManager(4096) // 80% of 2868 = 2294
	if threshold := m.CompactionThreshold(); threshold != 2294 {
		t.Fatalf("expected compaction threshold 2294, got %d", threshold)
	}
}

func TestHistoryTotalTokens(t *testing.T) {
	tx := NewTranscript()
	tx.AddGroup(HistoryGroup{ID: "a", Tokens: 100})
	tx.AddGroup(HistoryGroup{ID: "b", Tokens: 200})
	if total := tx.TotalTokens(); total != 300 {
		t.Fatalf("expected 300 total tokens, got %d", total)
	}
}

func TestHistoryRemoveGroup(t *testing.T) {
	tx := NewTranscript()
	tx.AddGroup(HistoryGroup{ID: "a", Tokens: 100})
	tx.AddGroup(HistoryGroup{ID: "b", Tokens: 200})
	if !tx.RemoveGroup("a") {
		t.Fatal("expected to remove group a")
	}
	if tx.TotalTokens() != 200 {
		t.Fatalf("expected 200 total after removal, got %d", tx.TotalTokens())
	}
	if tx.RemoveGroup("nonexistent") {
		t.Fatal("expected false for nonexistent group")
	}
}

func TestHistoryMarkWriteBody(t *testing.T) {
	tx := NewTranscript()
	tx.AddGroup(HistoryGroup{ID: "a", Tokens: 100, Evictable: false})
	if !tx.MarkWriteBody("a") {
		t.Fatal("expected to mark write body")
	}
	g := tx.Find("a")
	if g == nil || !g.Evictable {
		t.Fatal("expected write body group to be evictable")
	}
}
