package models

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseVeniceModelsFromFixture(t *testing.T) {
	fixtureData, err := os.ReadFile("../../testdata/models.json")
	if err != nil {
		t.Fatalf("read testdata/models.json: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("path = %q, want /models", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixtureData)
	}))
	defer server.Close()

	svc := NewService(server.Client(), server.URL, 10800)
	resp, err := svc.RegisterModels(context.Background(), RegistrationRequest{})
	if err != nil {
		t.Fatalf("RegisterModels: %v", err)
	}

	if resp.Provider != "venice" {
		t.Fatalf("provider = %q, want venice", resp.Provider)
	}
	if len(resp.Models) == 0 {
		t.Fatal("expected models from fixture, got 0")
	}

	// Verify specific model parsing
	var gemini4 *ModelInfo
	for i := range resp.Models {
		if resp.Models[i].ID == "gemini-4-argon" {
			gemini4 = &resp.Models[i]
			break
		}
	}
	if gemini4 == nil {
		t.Fatal("model gemini-4-argon not found")
	}
	if gemini4.DisplayName != "Gemini 4 Argon" {
		t.Errorf("displayName = %q, want 'Gemini 4 Argon'", gemini4.DisplayName)
	}
	if gemini4.ContextLength != 1000000 {
		t.Errorf("contextLength = %d, want 1000000", gemini4.ContextLength)
	}
	if gemini4.Thinking == nil {
		t.Error("expected thinking support on gemini-4-argon, got nil")
	}
}

func TestModelCacheAndReconfigureInvalidation(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"model-1","model_spec":{"name":"Model 1","availableContextTokens":4096}}]}`))
	}))
	defer server.Close()

	svc := NewService(server.Client(), server.URL, 1000)

	// Call 1: fetches from server
	resp1, err := svc.RegisterModels(context.Background(), RegistrationRequest{})
	if err != nil || len(resp1.Models) != 1 {
		t.Fatalf("call 1 failed: %v", err)
	}
	if atomic.LoadInt32(&requestCount) != 1 {
		t.Fatalf("requestCount = %d, want 1", requestCount)
	}

	// Call 2: should hit in-memory cache
	resp2, err := svc.RegisterModels(context.Background(), RegistrationRequest{})
	if err != nil || len(resp2.Models) != 1 {
		t.Fatalf("call 2 failed: %v", err)
	}
	if atomic.LoadInt32(&requestCount) != 1 {
		t.Fatalf("requestCount = %d, want 1 (cache hit)", requestCount)
	}

	// Invalidate (simulating plugin.reconfigure)
	svc.Invalidate()

	// Call 3: should refetch
	resp3, err := svc.RegisterModels(context.Background(), RegistrationRequest{})
	if err != nil || len(resp3.Models) != 1 {
		t.Fatalf("call 3 failed: %v", err)
	}
	if atomic.LoadInt32(&requestCount) != 2 {
		t.Fatalf("requestCount = %d, want 2 after invalidation", requestCount)
	}
}

func TestStaleCacheServedOnUpstreamFailure(t *testing.T) {
	shouldFail := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if shouldFail {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"upstream down"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"model-stable","model_spec":{"name":"Stable"}}]}`))
	}))
	defer server.Close()

	svc := NewService(server.Client(), server.URL, 1) // 1 second TTL

	// Initial fetch
	_, err := svc.RegisterModels(context.Background(), RegistrationRequest{})
	if err != nil {
		t.Fatalf("initial fetch failed: %v", err)
	}

	// Sleep past TTL to expire cache
	time.Sleep(1100 * time.Millisecond)

	// Upstream now fails
	shouldFail = true

	// Should still succeed by returning cached stale data
	resp, err := svc.RegisterModels(context.Background(), RegistrationRequest{})
	if err != nil {
		t.Fatalf("expected stale cache, got error: %v", err)
	}
	if len(resp.Models) != 1 || resp.Models[0].ID != "model-stable" {
		t.Fatalf("unexpected stale models: %+v", resp.Models)
	}
}

func TestConcurrentCacheExpiryTriggersOneRefresh(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		time.Sleep(20 * time.Millisecond) // Simulate network latency
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"model-1","model_spec":{"name":"Model 1"}}]}`))
	}))
	defer server.Close()

	svc := NewService(server.Client(), server.URL, 1)

	// Invalidate to start cold
	svc.Invalidate()

	const concurrentReaders = 50
	var wg sync.WaitGroup
	errCh := make(chan error, concurrentReaders)

	for i := 0; i < concurrentReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := svc.RegisterModels(context.Background(), RegistrationRequest{})
			if err != nil {
				errCh <- err
				return
			}
			if len(res.Models) != 1 {
				errCh <- fmt.Errorf("unexpected models count: %d", len(res.Models))
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("reader error: %v", err)
	}

	if count := atomic.LoadInt32(&requestCount); count != 1 {
		t.Fatalf("expected exactly 1 upstream refresh under 50 concurrent readers, got %d", count)
	}
}
