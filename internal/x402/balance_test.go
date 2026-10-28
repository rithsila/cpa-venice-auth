package x402

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBalanceGuardCanConsumeTrue(t *testing.T) {
	signer, err := NewSigner(testPrivKeyHex)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}

	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)

		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/x402/balance/"+signer.Address()) {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		authHeader := r.Header.Get("SIGN-IN-WITH-X")
		if authHeader == "" {
			t.Errorf("missing SIGN-IN-WITH-X header")
		}

		w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(BalanceInfo{
			CanConsume: true,
			BalanceUSD: 10.50,
		})
	}))
	defer server.Close()

	guard := NewBalanceGuard()

	// First call should query server
	err = guard.EnsureBalance(context.Background(), server.Client(), server.URL, signer)
	if err != nil {
		t.Fatalf("EnsureBalance failed: %v", err)
	}

	bal, canConsume, checked := guard.Balance()
	if !checked || !canConsume || bal != 10.50 {
		t.Fatalf("unexpected balance state: bal=%v, canConsume=%v, checked=%v", bal, canConsume, checked)
	}

	// Second call should reuse cached status
	err = guard.EnsureBalance(context.Background(), server.Client(), server.URL, signer)
	if err != nil {
		t.Fatalf("second EnsureBalance failed: %v", err)
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Fatalf("expected 1 server call, got %d", atomic.LoadInt32(&callCount))
	}
}

func TestBalanceGuardCanConsumeFalseRefuses(t *testing.T) {
	signer, err := NewSigner(testPrivKeyHex)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(BalanceInfo{
			CanConsume: false,
			BalanceUSD: 0.0,
		})
	}))
	defer server.Close()

	guard := NewBalanceGuard()
	err = guard.EnsureBalance(context.Background(), server.Client(), server.URL, signer)
	if err == nil {
		t.Fatal("expected EnsureBalance to fail when canConsume is false")
	}
	if !strings.Contains(err.Error(), "insufficient balance") {
		t.Fatalf("expected error mentioning insufficient balance, got: %v", err)
	}
}

func TestBalanceGuardUpdateFromHeader(t *testing.T) {
	guard := NewBalanceGuard()

	h1 := make(http.Header)
	h1.Set("X-Balance-Remaining", "4.85")
	guard.UpdateFromHeader(h1)

	bal, canConsume, checked := guard.Balance()
	if !checked || !canConsume || bal != 4.85 {
		t.Fatalf("expected 4.85, canConsume true; got bal=%v canConsume=%v", bal, canConsume)
	}

	// Depleted balance updates canConsume to false
	h2 := make(http.Header)
	h2.Set("x-balance-remaining", "0.00")
	guard.UpdateFromHeader(h2)

	bal, canConsume, _ = guard.Balance()
	if canConsume || bal != 0.0 {
		t.Fatalf("expected 0.0, canConsume false; got bal=%v canConsume=%v", bal, canConsume)
	}
}
