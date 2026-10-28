package x402

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// BalanceInfo represents the JSON response from /api/v1/x402/balance/{address}.
type BalanceInfo struct {
	CanConsume        bool    `json:"canConsume"`
	BalanceUSD        float64 `json:"balanceUsd"`
	MinimumTopUpUSD   float64 `json:"minimumTopUpUsd"`
	SuggestedTopUpUSD float64 `json:"suggestedTopUpUsd"`
	DiemBalanceUSD    float64 `json:"diemBalanceUsd"`
}

// BalanceGuard tracks x402 wallet balance and gates inference calls.
type BalanceGuard struct {
	mu         sync.RWMutex
	checked    bool
	canConsume bool
	balanceUSD float64
}

// NewBalanceGuard creates a fresh, thread-safe balance guard.
func NewBalanceGuard() *BalanceGuard {
	return &BalanceGuard{}
}

// Balance returns the current cached balance and consumption status.
func (b *BalanceGuard) Balance() (balanceUSD float64, canConsume bool, checked bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.balanceUSD, b.canConsume, b.checked
}

// Invalidate forces the next request to re-check balance against Venice.
func (b *BalanceGuard) Invalidate() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.checked = false
}

// UpdateFromHeader inspects response headers for X-Balance-Remaining and updates the balance state.
func (b *BalanceGuard) UpdateFromHeader(headers http.Header) {
	if headers == nil {
		return
	}

	valStr := headers.Get("X-Balance-Remaining")
	if valStr == "" {
		valStr = headers.Get("x-balance-remaining")
	}
	if valStr == "" {
		return
	}

	val, err := strconv.ParseFloat(strings.TrimSpace(valStr), 64)
	if err != nil {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.balanceUSD = val
	b.checked = true
	b.canConsume = val > 0
}

// EnsureBalance checks the wallet balance before first inference call.
// If canConsume is false, it returns a readable error instructing the user to top up.
func (b *BalanceGuard) EnsureBalance(ctx context.Context, client *http.Client, baseURL string, signer *Signer) error {
	b.mu.RLock()
	if b.checked && b.canConsume {
		b.mu.RUnlock()
		return nil
	}
	b.mu.RUnlock()

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.checked && b.canConsume {
		return nil
	}

	if client == nil {
		client = http.DefaultClient
	}

	endpoint := strings.TrimRight(baseURL, "/") + "/x402/balance/" + signer.Address()

	authHeader, err := signer.GenerateHeader()
	if err != nil {
		return fmt.Errorf("generate x402 auth header: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create balance request: %w", err)
	}
	req.Header.Set("SIGN-IN-WITH-X", authHeader)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("x402 balance check: %w", err)
	}
	defer resp.Body.Close()

	if date := resp.Header.Get("Date"); date != "" {
		_ = signer.UpdateClockSkew(date)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return fmt.Errorf("read balance response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("x402 balance check failed (status %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var info BalanceInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return fmt.Errorf("decode balance response: %w", err)
	}

	b.balanceUSD = info.BalanceUSD
	b.canConsume = info.CanConsume
	b.checked = true

	if !info.CanConsume {
		return fmt.Errorf("x402 wallet %s has insufficient balance (balance: $%.2f, canConsume: false): top up with USDC on Base to enable inference", signer.Address(), info.BalanceUSD)
	}

	return nil
}
