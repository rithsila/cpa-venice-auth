package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseAuthRejectsForeignFiles(t *testing.T) {
	svc := NewService(nil, DefaultConfig())
	req := ParseRequest{
		Provider: "other-provider",
		FileName: "other.json",
		RawJSON:  []byte(`{"type":"openai","api_key":"sk-12345"}`),
	}

	resp, err := svc.Parse(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Handled {
		t.Fatal("handled = true, want false for non-venice file")
	}
}

func TestParseAuthAcceptsVeniceRawJSON(t *testing.T) {
	svc := NewService(nil, DefaultConfig())
	raw := []byte(`{
		"type": "venice",
		"api_key": "vnc_test_key_12345",
		"label": "My Venice Key"
	}`)
	req := ParseRequest{
		FileName: "venice.json",
		RawJSON:  raw,
	}

	resp, err := svc.Parse(context.Background(), req)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if !resp.Handled {
		t.Fatal("handled = false, want true")
	}
	if resp.Auth.Provider != "venice" {
		t.Fatalf("provider = %q, want venice", resp.Auth.Provider)
	}
	if resp.Auth.Label != "My Venice Key" {
		t.Fatalf("label = %q, want 'My Venice Key'", resp.Auth.Label)
	}
	if resp.Auth.ID != "venice" {
		t.Fatalf("id = %q, want 'venice'", resp.Auth.ID)
	}

	var storage Storage
	if err := json.Unmarshal(resp.Auth.StorageJSON, &storage); err != nil {
		t.Fatalf("unmarshal storage: %v", err)
	}
	if storage.APIKey != "vnc_test_key_12345" {
		t.Fatalf("storage.api_key = %q, want vnc_test_key_12345", storage.APIKey)
	}
	if storage.Type != "venice" {
		t.Fatalf("storage.type = %q, want venice", storage.Type)
	}
}

func TestParseAuthAcceptsCPAStorageFormat(t *testing.T) {
	svc := NewService(nil, DefaultConfig())
	storageJSON, _ := json.Marshal(Storage{
		Type:   "venice",
		APIKey: "vnc_secret_abc",
	})
	raw, _ := json.Marshal(map[string]any{
		"Provider":    "venice",
		"ID":          "venice-prod",
		"FileName":    "venice-prod.json",
		"Label":       "Production Venice",
		"StorageJSON": storageJSON,
	})

	resp, err := svc.Parse(context.Background(), ParseRequest{
		Provider: "venice",
		FileName: "venice-prod.json",
		RawJSON:  raw,
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !resp.Handled {
		t.Fatal("handled = false, want true")
	}
	if resp.Auth.ID != "venice-prod" {
		t.Fatalf("id = %q, want venice-prod", resp.Auth.ID)
	}

	var storage Storage
	if err := json.Unmarshal(resp.Auth.StorageJSON, &storage); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if storage.APIKey != "vnc_secret_abc" {
		t.Fatalf("key = %q", storage.APIKey)
	}
}

func TestRefreshSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api_keys/rate_limits" {
			t.Errorf("path = %q, want /api_keys/rate_limits", r.URL.Path)
		}
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer vnc_valid_key" {
			t.Errorf("auth header = %q", authHeader)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"tier":"paid","limits":{}}}`))
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.BaseURL = server.URL
	svc := NewService(server.Client(), cfg)

	storageJSON, _ := json.Marshal(Storage{
		Type:   "venice",
		APIKey: "vnc_valid_key",
	})

	req := RefreshRequest{
		AuthID:       "venice-key-1",
		AuthProvider: "venice",
		StorageJSON:  storageJSON,
	}

	resp, err := svc.Refresh(context.Background(), req)
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}
	if resp.Auth.Provider != "venice" {
		t.Fatalf("provider = %q", resp.Auth.Provider)
	}
	if resp.NextRefreshAfter.Before(time.Now().Add(5 * time.Hour)) {
		t.Fatalf("next refresh too soon: %v", resp.NextRefreshAfter)
	}
}

func TestRefreshUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"Invalid API key"}`))
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.BaseURL = server.URL
	svc := NewService(server.Client(), cfg)

	storageJSON, _ := json.Marshal(Storage{
		Type:   "venice",
		APIKey: "vnc_bad_key",
	})

	_, err := svc.Refresh(context.Background(), RefreshRequest{
		AuthID:       "venice-key-1",
		AuthProvider: "venice",
		StorageJSON:  storageJSON,
	})
	if err == nil {
		t.Fatal("expected error on 401, got nil")
	}
	if !strings.Contains(err.Error(), "unauthorized") && !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestLoginStart(t *testing.T) {
	svc := NewService(nil, DefaultConfig())
	resp, err := svc.StartLogin(context.Background(), LoginStartRequest{Provider: "venice"})
	if err != nil {
		t.Fatalf("StartLogin failed: %v", err)
	}
	if resp.Provider != "venice" {
		t.Fatalf("provider = %q, want venice", resp.Provider)
	}
	if resp.URL != "https://venice.ai/settings/api" {
		t.Fatalf("url = %q, want https://venice.ai/settings/api", resp.URL)
	}
	if resp.State == "" {
		t.Fatal("state is empty")
	}
	if resp.ExpiresAt.Before(time.Now().Add(5 * time.Minute)) {
		t.Fatalf("expiresAt too soon: %v", resp.ExpiresAt)
	}
}

func TestLoginPoll(t *testing.T) {
	svc := NewService(nil, DefaultConfig())
	resp, err := svc.PollLogin(context.Background(), LoginPollRequest{
		Provider: "venice",
		State:    "test-state",
	})
	if err != nil {
		t.Fatalf("PollLogin failed: %v", err)
	}
	if resp.Status != "pending" {
		t.Fatalf("status = %q, want pending", resp.Status)
	}
	if resp.Message == "" {
		t.Fatal("message is empty")
	}
}

func FuzzAuthParse(f *testing.F) {
	// Seed corpus with various valid and invalid auth structures
	f.Add([]byte(`{"type":"venice","api_key":"vnc_123"}`))
	f.Add([]byte(`{"provider":"venice","wallet_key":"0x1234"}`))
	f.Add([]byte(`{"StorageJSON":"eyJ0eXBlIjoidmVuaWNlIiwiYXBpX2tleSI6InRlc3QifQ=="}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{"type":"openai","key":"sk-123"}`))
	f.Add([]byte(`{"id":"venice-custom","label":"Custom Venice","apiKey":"key999"}`))
	f.Add([]byte(`{"type":"venice","mode":"wallet","wallet_key":"abc"}`))
	f.Add([]byte(`garbage bytes and []}{:`))

	svc := NewService(nil, DefaultConfig())

	f.Fuzz(func(t *testing.T, data []byte) {
		// Parse should never crash on random byte inputs
		res, err := svc.Parse(context.Background(), ParseRequest{
			Provider: "venice",
			FileName: "test.json",
			RawJSON:  data,
		})
		if err == nil && res.Handled {
			_ = res.Auth.ID
			_ = res.Auth.Label
			_ = res.Auth.StorageJSON
		}
	})
}
