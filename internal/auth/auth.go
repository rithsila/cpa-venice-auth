package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"cpa-venice-auth/internal/dispatch"
)

// Service implements Venice auth provider operations.
type Service struct {
	client *http.Client
	cfg    Config
}

// NewService creates a new Venice auth provider service.
func NewService(client *http.Client, cfg Config) *Service {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.venice.ai/api/v1"
	}
	if cfg.DefaultRefreshTTL <= 0 {
		cfg.DefaultRefreshTTL = 6 * time.Hour
	}
	return &Service{client: client, cfg: cfg}
}

// Parse extracts credentials and builds an Auth record.
func (s *Service) Parse(ctx context.Context, req ParseRequest) (ParseResponse, error) {
	isVenice := strings.EqualFold(req.Provider, "venice")

	var rawMap map[string]any
	if len(req.RawJSON) > 0 {
		if err := json.Unmarshal(req.RawJSON, &rawMap); err == nil {
			if t, ok := rawMap["type"].(string); ok && strings.EqualFold(t, "venice") {
				isVenice = true
			}
			if p, ok := rawMap["Provider"].(string); ok && strings.EqualFold(p, "venice") {
				isVenice = true
			}
			if p, ok := rawMap["provider"].(string); ok && strings.EqualFold(p, "venice") {
				isVenice = true
			}
		}
	}

	if !isVenice {
		return ParseResponse{Handled: false}, nil
	}

	var storage Storage
	storage.Type = "venice"
	storage.BaseURL = s.cfg.BaseURL
	storage.Mode = "api_key"

	var id, label, fileName string
	fileName = req.FileName

	// 1. Check if raw JSON already has StorageJSON (CPA export format)
	if sJSON, ok := rawMap["StorageJSON"]; ok {
		switch v := sJSON.(type) {
		case string:
			decoded, err := base64.StdEncoding.DecodeString(v)
			if err == nil {
				_ = json.Unmarshal(decoded, &storage)
			} else {
				_ = json.Unmarshal([]byte(v), &storage)
			}
		case []byte:
			_ = json.Unmarshal(v, &storage)
		case map[string]any:
			mBytes, _ := json.Marshal(v)
			_ = json.Unmarshal(mBytes, &storage)
		}
	}

	// 2. Extract top-level keys if StorageJSON didn't yield an APIKey
	if storage.APIKey == "" {
		if k, ok := rawMap["api_key"].(string); ok {
			storage.APIKey = strings.TrimSpace(k)
		} else if k, ok := rawMap["apiKey"].(string); ok {
			storage.APIKey = strings.TrimSpace(k)
		} else if k, ok := rawMap["token"].(string); ok {
			storage.APIKey = strings.TrimSpace(k)
		}
	}
	if storage.WalletKey == "" {
		if w, ok := rawMap["wallet_key"].(string); ok {
			storage.WalletKey = strings.TrimSpace(w)
		}
	}
	if storage.WalletKey != "" && storage.APIKey == "" {
		storage.Mode = "wallet"
	}

	if val, ok := rawMap["ID"].(string); ok && strings.TrimSpace(val) != "" {
		id = strings.TrimSpace(val)
	} else if val, ok := rawMap["id"].(string); ok && strings.TrimSpace(val) != "" {
		id = strings.TrimSpace(val)
	}

	if val, ok := rawMap["Label"].(string); ok && strings.TrimSpace(val) != "" {
		label = strings.TrimSpace(val)
	} else if val, ok := rawMap["label"].(string); ok && strings.TrimSpace(val) != "" {
		label = strings.TrimSpace(val)
	}

	if id == "" {
		if fileName != "" {
			ext := filepath.Ext(fileName)
			id = strings.TrimSuffix(fileName, ext)
		} else {
			id = "venice"
		}
	}

	if label == "" {
		if storage.APIKey != "" {
			label = fmt.Sprintf("Venice (%s)", maskKey(storage.APIKey))
		} else if storage.WalletKey != "" {
			label = "Venice (Wallet)"
		} else {
			label = "Venice"
		}
	}

	storageBytes, err := json.Marshal(storage)
	if err != nil {
		return ParseResponse{}, fmt.Errorf("marshal storage: %w", err)
	}

	authData := AuthData{
		Provider:         "venice",
		ID:               id,
		FileName:         fileName,
		Label:            label,
		StorageJSON:      storageBytes,
		Metadata:         map[string]any{"type": "venice", "mode": storage.Mode},
		Attributes:       map[string]string{"type": "venice"},
		NextRefreshAfter: time.Now().Add(s.cfg.DefaultRefreshTTL),
	}

	return ParseResponse{
		Handled: true,
		Auth:    authData,
	}, nil
}

// Refresh checks the credential validity against Venice API.
func (s *Service) Refresh(ctx context.Context, req RefreshRequest) (RefreshResponse, error) {
	var storage Storage
	if len(req.StorageJSON) > 0 {
		if err := json.Unmarshal(req.StorageJSON, &storage); err != nil {
			return RefreshResponse{}, dispatch.Errorf("auth_invalid", "decode storage: %v", err)
		}
	}

	if storage.APIKey == "" && storage.WalletKey == "" {
		return RefreshResponse{}, dispatch.Errorf("auth_invalid", "no api_key or wallet_key found in auth data")
	}

	// In API key mode, validate against /api_keys/rate_limits
	if storage.APIKey != "" {
		baseURL := s.cfg.BaseURL
		if storage.BaseURL != "" {
			baseURL = storage.BaseURL
		}
		endpoint := strings.TrimRight(baseURL, "/") + "/api_keys/rate_limits"

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return RefreshResponse{}, fmt.Errorf("create refresh request: %w", err)
		}
		httpReq.Header.Set("Authorization", "Bearer "+storage.APIKey)
		httpReq.Header.Set("Accept", "application/json")

		resp, err := s.client.Do(httpReq)
		if err != nil {
			return RefreshResponse{}, fmt.Errorf("venice rate limits check: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			return RefreshResponse{}, dispatch.Errorf("auth_invalid", "venice unauthorized (status %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			return RefreshResponse{}, dispatch.Errorf("rate_limited", "venice rate limit reached during refresh")
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			return RefreshResponse{}, dispatch.Errorf("upstream_error", "venice refresh error (status %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
	}

	nextRefresh := time.Now().Add(s.cfg.DefaultRefreshTTL)
	authData := AuthData{
		Provider:         "venice",
		ID:               req.AuthID,
		StorageJSON:      req.StorageJSON,
		Metadata:         req.Metadata,
		Attributes:       req.Attributes,
		NextRefreshAfter: nextRefresh,
	}

	return RefreshResponse{
		Auth:             authData,
		NextRefreshAfter: nextRefresh,
	}, nil
}

// StartLogin initiates the paste-key login flow.
func (s *Service) StartLogin(ctx context.Context, req LoginStartRequest) (LoginStartResponse, error) {
	state := generateState()
	expiresAt := time.Now().Add(15 * time.Minute)

	return LoginStartResponse{
		Provider:  "venice",
		URL:       "https://venice.ai/settings/api",
		State:     state,
		ExpiresAt: expiresAt,
		Metadata: map[string]any{
			"instructions": "Open Venice settings, generate an API key, then import it into CLIProxyAPI.",
			"mode":         "paste_key",
		},
	}, nil
}

// PollLogin returns a pending status for manual paste-key flows.
func (s *Service) PollLogin(ctx context.Context, req LoginPollRequest) (LoginPollResponse, error) {
	return LoginPollResponse{
		Status:  "pending",
		Message: "Venice uses manual API key generation. Generate a key at https://venice.ai/settings/api and paste it into CLIProxyAPI.",
	}, nil
}

func maskKey(key string) string {
	if len(key) <= 8 {
		return "..."
	}
	return key[:4] + "..." + key[len(key)-4:]
}

func generateState() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return "venice-auth-" + hex.EncodeToString(buf)
}
