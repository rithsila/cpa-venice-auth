package auth

import (
	"encoding/base64"
	"encoding/json"
	"time"
)

// RawBytes unmarshals from either a base64-encoded JSON string or a raw JSON value.
type RawBytes []byte

// UnmarshalJSON implements json.Unmarshaler.
func (r *RawBytes) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*r = nil
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err == nil {
			if decoded, err := base64.StdEncoding.DecodeString(s); err == nil {
				*r = decoded
				return nil
			}
			*r = []byte(s)
			return nil
		}
	}
	*r = append((*r)[0:0], data...)
	return nil
}

// Storage is the provider-owned data persisted inside AuthData.StorageJSON.
type Storage struct {
	Type      string `json:"type"`
	Mode      string `json:"mode,omitempty"`
	APIKey    string `json:"api_key,omitempty"`
	WalletKey string `json:"wallet_key,omitempty"`
	BaseURL   string `json:"base_url,omitempty"`
}

// AuthData matches the wire format of CPA sdk/pluginapi.AuthData.
type AuthData struct {
	Provider         string            `json:"Provider"`
	ID               string            `json:"ID"`
	FileName         string            `json:"FileName,omitempty"`
	Label            string            `json:"Label,omitempty"`
	Prefix           string            `json:"Prefix,omitempty"`
	ProxyURL         string            `json:"ProxyURL,omitempty"`
	Disabled         bool              `json:"Disabled,omitempty"`
	StorageJSON      []byte            `json:"StorageJSON"`
	Metadata         map[string]any    `json:"Metadata,omitempty"`
	Attributes       map[string]string `json:"Attributes,omitempty"`
	NextRefreshAfter time.Time         `json:"NextRefreshAfter,omitempty"`
}

// ParseRequest mirrors pluginapi.AuthParseRequest.
type ParseRequest struct {
	Provider string   `json:"Provider"`
	Path     string   `json:"Path"`
	FileName string   `json:"FileName"`
	RawJSON  RawBytes `json:"RawJSON"`
}

// ParseResponse mirrors pluginapi.AuthParseResponse.
type ParseResponse struct {
	Handled bool       `json:"Handled"`
	Auth    AuthData   `json:"Auth"`
	Auths   []AuthData `json:"Auths,omitempty"`
}

// RefreshRequest mirrors pluginapi.AuthRefreshRequest.
type RefreshRequest struct {
	AuthID       string            `json:"AuthID"`
	AuthProvider string            `json:"AuthProvider"`
	StorageJSON  RawBytes          `json:"StorageJSON"`
	Metadata     map[string]any    `json:"Metadata,omitempty"`
	Attributes   map[string]string `json:"Attributes,omitempty"`
}

// RefreshResponse mirrors pluginapi.AuthRefreshResponse.
type RefreshResponse struct {
	Auth             AuthData  `json:"Auth"`
	NextRefreshAfter time.Time `json:"NextRefreshAfter"`
}

// LoginStartRequest mirrors pluginapi.AuthLoginStartRequest.
type LoginStartRequest struct {
	Provider string         `json:"Provider"`
	BaseURL  string         `json:"BaseURL"`
	Metadata map[string]any `json:"Metadata,omitempty"`
}

// LoginStartResponse mirrors pluginapi.AuthLoginStartResponse.
type LoginStartResponse struct {
	Provider  string         `json:"Provider"`
	URL       string         `json:"URL"`
	State     string         `json:"State"`
	ExpiresAt time.Time      `json:"ExpiresAt"`
	Metadata  map[string]any `json:"Metadata,omitempty"`
}

// LoginPollRequest mirrors pluginapi.AuthLoginPollRequest.
type LoginPollRequest struct {
	Provider string         `json:"Provider"`
	State    string         `json:"State"`
	Metadata map[string]any `json:"Metadata,omitempty"`
}

// LoginPollResponse mirrors pluginapi.AuthLoginPollResponse.
type LoginPollResponse struct {
	Status  string     `json:"Status"`
	Message string     `json:"Message,omitempty"`
	Auth    AuthData   `json:"Auth,omitempty"`
	Auths   []AuthData `json:"Auths,omitempty"`
}

// Config controls auth provider defaults.
type Config struct {
	BaseURL            string
	DefaultRefreshTTL  time.Duration
	ModelsCacheSeconds int
}

// DefaultConfig provides default settings.
func DefaultConfig() Config {
	return Config{
		BaseURL:           "https://api.venice.ai/api/v1",
		DefaultRefreshTTL: 6 * time.Hour,
	}
}
