package executor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestExecuteNonStreamingSuccess(t *testing.T) {
	upstreamResponse := `{"id":"vnc-123","object":"chat.completion","model":"llama-3.3-70b","choices":[{"message":{"role":"assistant","content":"hello"}}]}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q, want /chat/completions", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-api-key" {
			t.Errorf("auth header = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(upstreamResponse))
	}))
	defer server.Close()

	svc := NewService(server.Client(), nil, Config{BaseURL: server.URL})

	storageBytes, _ := json.Marshal(map[string]any{
		"type":    "venice",
		"api_key": "test-api-key",
	})

	req := Request{
		Model:       "llama-3.3-70b",
		Payload:     []byte(`{"model":"llama-3.3-70b","messages":[{"role":"user","content":"hi"}]}`),
		StorageJSON: storageBytes,
	}

	resp, err := svc.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if string(resp.Payload) != upstreamResponse {
		t.Fatalf("payload = %s, want %s", string(resp.Payload), upstreamResponse)
	}
}

func TestExecutePreservesVeniceParameters(t *testing.T) {
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"ok"}`))
	}))
	defer server.Close()

	svc := NewService(server.Client(), nil, Config{BaseURL: server.URL})

	storageBytes, _ := json.Marshal(map[string]any{"api_key": "test-key"})
	inputJSON := `{
		"model": "llama-3.3-70b",
		"messages": [{"role":"user","content":"hi"}],
		"venice_parameters": {
			"include_venice_system_prompt": false,
			"enable_web_search": "on"
		}
	}`

	_, err := svc.Execute(context.Background(), Request{
		Model:       "llama-3.3-70b",
		Payload:     []byte(inputJSON),
		StorageJSON: storageBytes,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	vp, ok := receivedBody["venice_parameters"].(map[string]any)
	if !ok {
		t.Fatal("venice_parameters was dropped from upstream request")
	}
	if vp["include_venice_system_prompt"] != false {
		t.Errorf("include_venice_system_prompt = %v, want false", vp["include_venice_system_prompt"])
	}
	if vp["enable_web_search"] != "on" {
		t.Errorf("enable_web_search = %v, want 'on'", vp["enable_web_search"])
	}
}

func TestExecuteMaps402InsufficientBalance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"error":{"message":"Insufficient balance","type":"INSUFFICIENT_BALANCE"}}`))
	}))
	defer server.Close()

	svc := NewService(server.Client(), nil, Config{BaseURL: server.URL})
	storageBytes, _ := json.Marshal(map[string]any{"api_key": "test-key"})

	_, err := svc.Execute(context.Background(), Request{
		Model:       "llama-3.3-70b",
		Payload:     []byte(`{"messages":[]}`),
		StorageJSON: storageBytes,
	})
	if err == nil {
		t.Fatal("expected error on 402, got nil")
	}

	fault, ok := err.(interface {
		FaultCode() string
		StatusCode() int
	})
	if !ok {
		t.Fatalf("error does not implement Fault interface: %v", err)
	}
	if fault.StatusCode() != http.StatusPaymentRequired {
		t.Fatalf("status = %d, want 402", fault.StatusCode())
	}
	if fault.FaultCode() != "insufficient_balance" {
		t.Fatalf("code = %q, want insufficient_balance", fault.FaultCode())
	}
}

func TestExecuteMaps401ProOnlyModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"PRO_ONLY_MODEL: this model requires a Pro subscription"}`))
	}))
	defer server.Close()

	svc := NewService(server.Client(), nil, Config{BaseURL: server.URL})
	storageBytes, _ := json.Marshal(map[string]any{"api_key": "test-key"})

	_, err := svc.Execute(context.Background(), Request{
		Model:       "pro-model",
		Payload:     []byte(`{"messages":[]}`),
		StorageJSON: storageBytes,
	})
	if err == nil {
		t.Fatal("expected error on 401 PRO_ONLY_MODEL, got nil")
	}

	fault, ok := err.(interface {
		FaultCode() string
		StatusCode() int
	})
	if !ok {
		t.Fatalf("error does not implement Fault interface: %v", err)
	}
	if fault.FaultCode() != "model_unavailable" {
		t.Fatalf("code = %q, want model_unavailable", fault.FaultCode())
	}
}

func TestExecuteMaps429RateLimitedWithRetryAfter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "12")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"Rate limit reached"}`))
	}))
	defer server.Close()

	svc := NewService(server.Client(), nil, Config{BaseURL: server.URL})
	storageBytes, _ := json.Marshal(map[string]any{"api_key": "test-key"})

	_, err := svc.Execute(context.Background(), Request{
		Model:       "llama-3.3-70b",
		Payload:     []byte(`{"messages":[]}`),
		StorageJSON: storageBytes,
	})
	if err == nil {
		t.Fatal("expected error on 429, got nil")
	}

	fault, ok := err.(interface {
		FaultCode() string
		StatusCode() int
		RetryAfterSeconds() int
	})
	if !ok {
		t.Fatalf("error does not implement Fault interface: %v", err)
	}
	if fault.StatusCode() != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", fault.StatusCode())
	}
	if fault.RetryAfterSeconds() != 12 {
		t.Fatalf("retry after = %d, want 12", fault.RetryAfterSeconds())
	}
}

func TestExecuteStreamSSEEmitsChunks(t *testing.T) {
	ssePayload := "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\" world\"}}]}\n\ndata: [DONE]\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(ssePayload))
	}))
	defer server.Close()

	var emittedChunks []string
	var mu sync.Mutex
	closed := false

	mockHostCall := func(method string, payload []byte) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		if method == "host.stream.emit" {
			var req struct {
				StreamID string `json:"stream_id"`
				Payload  []byte `json:"payload"`
			}
			_ = json.Unmarshal(payload, &req)
			emittedChunks = append(emittedChunks, string(req.Payload))
		}
		if method == "host.stream.close" {
			closed = true
		}
		return []byte(`{}`), nil
	}

	svc := NewService(server.Client(), mockHostCall, Config{BaseURL: server.URL})
	storageBytes, _ := json.Marshal(map[string]any{"api_key": "test-key"})

	resp, err := svc.ExecuteStream(context.Background(), Request{
		Model:       "llama-3.3-70b",
		StreamID:    "stream-abc-123",
		Payload:     []byte(`{"stream":true,"messages":[]}`),
		StorageJSON: storageBytes,
	})
	if err != nil {
		t.Fatalf("ExecuteStream failed: %v", err)
	}
	if resp.Headers.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("content type = %q, want text/event-stream", resp.Headers.Get("Content-Type"))
	}

	// Wait briefly for background stream forwarding to complete
	for i := 0; i < 50; i++ {
		mu.Lock()
		done := closed
		mu.Unlock()
		if done {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if !closed {
		t.Fatal("stream was not closed via host.stream.close")
	}
	combined := strings.Join(emittedChunks, "")
	if !strings.Contains(combined, "Hello") || !strings.Contains(combined, "world") {
		t.Fatalf("emitted chunks missing content: %v", emittedChunks)
	}
}

func TestCountTokensUnsupported(t *testing.T) {
	svc := NewService(nil, nil, Config{})
	_, err := svc.CountTokens(context.Background(), Request{})
	if err == nil {
		t.Fatal("expected error from CountTokens, got nil")
	}
	fault, ok := err.(interface{ FaultCode() string })
	if !ok || fault.FaultCode() != "unsupported" {
		t.Fatalf("fault code = %v, want unsupported", err)
	}
}

func TestExecuteWalletModeSignsWithXAndNoAuthorizationHeader(t *testing.T) {
	const testPrivKeyHex = "4c0883a69102937d6231471b5dbb6204db7e716b5c6e0f4504b0484389fa4009"
	const expectedAddress = "0xF01a4711A00D21f860fd58A4357F21Ee102f6214"

	var sawBalanceCheck bool
	var sawChatCall bool
	var chatSignHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))

		if strings.HasPrefix(r.URL.Path, "/x402/balance/") {
			sawBalanceCheck = true
			if !strings.HasSuffix(r.URL.Path, expectedAddress) {
				t.Errorf("balance path = %s, expected suffix %s", r.URL.Path, expectedAddress)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"canConsume":true,"balanceUsd":5.50}`))
			return
		}

		if r.URL.Path == "/chat/completions" {
			sawChatCall = true
			if r.Header.Get("Authorization") != "" {
				t.Errorf("expected no Authorization header in wallet mode, got %q", r.Header.Get("Authorization"))
			}
			chatSignHeader = r.Header.Get("SIGN-IN-WITH-X")
			if chatSignHeader == "" {
				t.Errorf("missing SIGN-IN-WITH-X header on chat completions request")
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Balance-Remaining", "5.45")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"chat-1","choices":[{"message":{"content":"ok"}}]}`))
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := NewService(server.Client(), nil, Config{
		BaseURL:       server.URL,
		WalletEnabled: true,
	})

	storageBytes, _ := json.Marshal(map[string]any{
		"type":       "venice",
		"mode":       "wallet",
		"wallet_key": testPrivKeyHex,
	})

	resp, err := svc.Execute(context.Background(), Request{
		Model:       "llama-3.3-70b",
		Payload:     []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		StorageJSON: storageBytes,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !sawBalanceCheck {
		t.Fatal("pre-flight balance check was not executed")
	}
	if !sawChatCall {
		t.Fatal("chat completions call was not executed")
	}
	if chatSignHeader == "" {
		t.Fatal("chat completions call did not receive SIGN-IN-WITH-X header")
	}

	bal, _, checked := svc.Balance()
	if !checked || bal != 5.45 {
		t.Errorf("balance was not updated from X-Balance-Remaining: bal=%v, checked=%v", bal, checked)
	}
	if !strings.Contains(string(resp.Payload), "chat-1") {
		t.Errorf("unexpected payload: %s", string(resp.Payload))
	}
}

func TestExecuteWalletModeRefusesWhenInsufficientBalance(t *testing.T) {
	const testPrivKeyHex = "4c0883a69102937d6231471b5dbb6204db7e716b5c6e0f4504b0484389fa4009"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/x402/balance/") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"canConsume":false,"balanceUsd":0.00}`))
			return
		}
		if r.URL.Path == "/chat/completions" {
			t.Fatal("chat completions should NOT be called when balance guard fails")
		}
	}))
	defer server.Close()

	svc := NewService(server.Client(), nil, Config{
		BaseURL:       server.URL,
		WalletEnabled: true,
	})

	storageBytes, _ := json.Marshal(map[string]any{
		"type":       "venice",
		"mode":       "wallet",
		"wallet_key": testPrivKeyHex,
	})

	_, err := svc.Execute(context.Background(), Request{
		Model:       "llama-3.3-70b",
		Payload:     []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		StorageJSON: storageBytes,
	})
	if err == nil {
		t.Fatal("expected Execute to fail due to insufficient balance")
	}

	fault, ok := err.(interface{ FaultCode() string })
	if !ok || fault.FaultCode() != "insufficient_balance" {
		t.Fatalf("expected insufficient_balance fault, got: %v", err)
	}
}

func TestExecuteWalletModeReSignsOnRetry(t *testing.T) {
	const testPrivKeyHex = "4c0883a69102937d6231471b5dbb6204db7e716b5c6e0f4504b0484389fa4009"

	var headersSeen []string
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))

		if strings.HasPrefix(r.URL.Path, "/x402/balance/") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"canConsume":true,"balanceUsd":10.00}`))
			return
		}

		if r.URL.Path == "/chat/completions" {
			mu.Lock()
			headersSeen = append(headersSeen, r.Header.Get("SIGN-IN-WITH-X"))
			callNum := len(headersSeen)
			mu.Unlock()

			if callNum == 1 {
				// First attempt fails with 503 Service Unavailable (triggering retry)
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error":"temporarily unavailable"}`))
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"chat-retry-ok"}`))
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := NewService(server.Client(), nil, Config{
		BaseURL:       server.URL,
		WalletEnabled: true,
	})

	storageBytes, _ := json.Marshal(map[string]any{
		"type":       "venice",
		"mode":       "wallet",
		"wallet_key": testPrivKeyHex,
	})

	resp, err := svc.Execute(context.Background(), Request{
		Model:       "llama-3.3-70b",
		Payload:     []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		StorageJSON: storageBytes,
	})
	if err != nil {
		t.Fatalf("Execute with retry failed: %v", err)
	}

	mu.Lock()
	count := len(headersSeen)
	h := append([]string(nil), headersSeen...)
	mu.Unlock()

	if count < 2 {
		t.Fatalf("expected at least 2 chat calls with retry, got %d", count)
	}

	if h[0] == h[1] {
		t.Fatal("SIGN-IN-WITH-X header was reused across retry attempts! Must be re-signed on every retry.")
	}

	if !strings.Contains(string(resp.Payload), "chat-retry-ok") {
		t.Fatalf("expected successful retry response, got: %s", string(resp.Payload))
	}
}
