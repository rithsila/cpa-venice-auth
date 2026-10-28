package executor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func BenchmarkExecutorOverhead(b *testing.B) {
	// Local server returns immediately with 200 JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chat-benchmark","choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	svc := NewService(server.Client(), nil, Config{BaseURL: server.URL})
	req := Request{
		Model:       "llama-3.3-70b",
		Payload:     []byte(`{"messages":[{"role":"user","content":"benchmark"}]}`),
		StorageJSON: []byte(`{"api_key":"test"}`),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := svc.Execute(context.Background(), req)
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
		if !strings.Contains(string(resp.Payload), "chat-benchmark") {
			b.Fatalf("unexpected payload: %s", string(resp.Payload))
		}
	}
}
