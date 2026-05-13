package trading

import (
	"context"
	"testing"

	"github.com/tiennm99/miti99bot/internal/storage"
)

func TestLoadPortfolio_FirstTimeUser(t *testing.T) {
	kv := storage.NewMemoryKVStore()
	p, err := LoadPortfolio(context.Background(), kv, 42, 1234567890)
	if err != nil {
		t.Fatalf("LoadPortfolio: %v", err)
	}
	if p.Currency["VND"] != 0 {
		t.Errorf("VND seeded: got %v, want 0", p.Currency["VND"])
	}
	if p.Assets == nil {
		t.Error("Assets is nil")
	}
	if p.Meta.CreatedAt != 1234567890 {
		t.Errorf("CreatedAt: got %d, want 1234567890", p.Meta.CreatedAt)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	kv := storage.NewMemoryKVStore()
	p, _ := LoadPortfolio(context.Background(), kv, 42, 1)
	p.AddCurrency("VND", 5_000_000)
	p.AddAsset("TCB", 100)
	p.Meta.Invested = 5_000_000
	if err := SavePortfolio(context.Background(), kv, 42, p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := LoadPortfolio(context.Background(), kv, 42, 999) // CreatedAt should NOT be reset
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Currency["VND"] != 5_000_000 {
		t.Errorf("VND: got %v, want 5000000", got.Currency["VND"])
	}
	if got.Assets["TCB"] != 100 {
		t.Errorf("TCB: got %d, want 100", got.Assets["TCB"])
	}
	if got.Meta.Invested != 5_000_000 {
		t.Errorf("Invested: got %v, want 5000000", got.Meta.Invested)
	}
	if got.Meta.CreatedAt != 1 {
		t.Errorf("CreatedAt: got %d, want 1 (load must NOT overwrite existing)", got.Meta.CreatedAt)
	}
}

func TestAddDeductCurrency(t *testing.T) {
	p := NewPortfolio(0)
	p.AddCurrency("VND", 1000)
	p.AddCurrency("VND", 500)
	if p.Currency["VND"] != 1500 {
		t.Errorf("after add: got %v, want 1500", p.Currency["VND"])
	}
	ok, bal := p.DeductCurrency("VND", 600)
	if !ok || bal != 900 {
		t.Errorf("deduct 600: ok=%v bal=%v, want ok=true bal=900", ok, bal)
	}
	ok, bal = p.DeductCurrency("VND", 9999)
	if ok || bal != 900 {
		t.Errorf("deduct over balance: ok=%v bal=%v, want ok=false bal=900 (unchanged)", ok, bal)
	}
}

func TestAddDeductAsset(t *testing.T) {
	p := NewPortfolio(0)
	p.AddAsset("TCB", 10)
	p.AddAsset("TCB", 5)
	if p.Assets["TCB"] != 15 {
		t.Errorf("TCB after add: got %d, want 15", p.Assets["TCB"])
	}
	ok, held := p.DeductAsset("TCB", 3)
	if !ok || held != 12 {
		t.Errorf("deduct 3: ok=%v held=%v, want ok=true held=12", ok, held)
	}
	ok, _ = p.DeductAsset("TCB", 999)
	if ok {
		t.Error("deduct over holdings: should fail")
	}
	// Final deduction removes key entirely.
	p.DeductAsset("TCB", 12)
	if _, present := p.Assets["TCB"]; present {
		t.Error("zero-balance asset should be removed from map")
	}
}
