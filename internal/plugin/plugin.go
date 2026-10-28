// Package plugin implements the Venice capability set.
package plugin

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"cpa-venice-auth/internal/auth"
	"cpa-venice-auth/internal/dispatch"
	"cpa-venice-auth/internal/executor"
	"cpa-venice-auth/internal/host"
	"cpa-venice-auth/internal/models"
	"cpa-venice-auth/internal/quota"
)

const (
	// ProviderName is the identifier CPA uses to route credentials and
	// requests to this plugin. The host lowercases it.
	ProviderName = "venice"
	// PluginID must match the key under plugins.configs.
	PluginID = "venice-provider"
	// Version is reported in registration metadata.
	Version = "0.1.0"
	// RepoURL is required by the host: validPlugin rejects a registration with
	// an empty GitHubRepository. Change this if the real repo lives elsewhere.
	RepoURL = "https://github.com/rithsila/cpa-venice-auth"
)

// Config is the plugin-owned configuration.
type Config struct {
	WalletEnabled      bool
	ModelsCacheSeconds int
	WalletKeyEnv       string
	BaseURL            string
}

// DefaultConfig returns the values used when a key is omitted.
func DefaultConfig() Config {
	return Config{
		ModelsCacheSeconds: 10800,
		WalletKeyEnv:       "VENICE_X402_PRIVATE_KEY",
		BaseURL:            "https://api.venice.ai/api/v1",
	}
}

// Venice holds runtime state for the provider.
type Venice struct {
	mu     sync.RWMutex
	cfg    Config
	host   *host.API
	auth   *auth.Service
	exec   *executor.Service
	models *models.Service
	quota  *quota.Service
	closed bool
}

// New builds a provider around a host API.
func New(h *host.API) *Venice {
	cfg := DefaultConfig()
	var hostCall host.CallFunc
	if h != nil {
		hostCall = h.Call
	}
	return &Venice{
		cfg:    cfg,
		host:   h,
		auth:   auth.NewService(nil, auth.Config{BaseURL: cfg.BaseURL}),
		exec: executor.NewService(nil, hostCall, executor.Config{
			BaseURL:       cfg.BaseURL,
			WalletEnabled: cfg.WalletEnabled,
			WalletKeyEnv:  cfg.WalletKeyEnv,
		}),
		models: models.NewService(nil, cfg.BaseURL, cfg.ModelsCacheSeconds),
		quota:  quota.NewService(nil, cfg.BaseURL),
	}
}

// Register installs every method this build supports. Methods for waves that
// are not implemented yet are deliberately absent, so the host gets a clean
// unknown_method instead of a half-working stub.
func (v *Venice) Register(d *dispatch.Dispatcher) {
	d.Register("plugin.register", v.handleRegister)
	d.Register("plugin.reconfigure", v.handleReconfigure)
	d.Register("auth.identifier", v.handleAuthIdentifier)
	d.Register("auth.parse", v.handleAuthParse)
	d.Register("auth.refresh", v.handleAuthRefresh)
	d.Register("auth.login.start", v.handleAuthLoginStart)
	d.Register("auth.login.poll", v.handleAuthLoginPoll)
	d.Register("executor.identifier", v.handleExecutorIdentifier)
	d.Register("executor.execute", v.handleExecutorExecute)
	d.Register("executor.execute_stream", v.handleExecutorExecuteStream)
	d.Register("executor.count_tokens", v.handleExecutorCountTokens)
	d.Register("executor.http_request", v.handleExecutorHTTPRequest)
	d.Register("model.register", v.handleModelRegister)
	d.Register("quota.identifier", v.handleQuotaIdentifier)
	d.Register("quota.describe", v.handleQuotaDescribe)
	d.Register("quota.fetch", v.handleQuotaFetch)
}

// Config returns a copy of the active configuration.
func (v *Venice) Config() Config {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.cfg
}

// Close stops background work and cancels any in-flight streams.
func (v *Venice) Close() {
	v.mu.Lock()
	v.closed = true
	v.mu.Unlock()

	if v.exec != nil {
		v.exec.Close(5 * time.Second)
	}
}

func (v *Venice) handleRegister(context.Context, []byte) (any, error) {
	return buildRegistration(), nil
}

func (v *Venice) handleAuthIdentifier(context.Context, []byte) (any, error) {
	return identifierResponse{Identifier: ProviderName}, nil
}

func (v *Venice) handleAuthParse(ctx context.Context, payload []byte) (any, error) {
	var req auth.ParseRequest
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &req)
	}
	return v.auth.Parse(ctx, req)
}

func (v *Venice) handleAuthRefresh(ctx context.Context, payload []byte) (any, error) {
	var req auth.RefreshRequest
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, dispatch.Errorf("invalid_request", "unmarshal refresh request: %v", err)
		}
	}
	return v.auth.Refresh(ctx, req)
}

func (v *Venice) handleAuthLoginStart(ctx context.Context, payload []byte) (any, error) {
	var req auth.LoginStartRequest
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &req)
	}
	return v.auth.StartLogin(ctx, req)
}

func (v *Venice) handleAuthLoginPoll(ctx context.Context, payload []byte) (any, error) {
	var req auth.LoginPollRequest
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &req)
	}
	return v.auth.PollLogin(ctx, req)
}

func (v *Venice) handleExecutorIdentifier(context.Context, []byte) (any, error) {
	return identifierResponse{Identifier: ProviderName}, nil
}

func (v *Venice) handleExecutorExecute(ctx context.Context, payload []byte) (any, error) {
	var req executor.Request
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, dispatch.Errorf("invalid_request", "unmarshal executor request: %v", err)
		}
	}
	return v.exec.Execute(ctx, req)
}

func (v *Venice) handleExecutorExecuteStream(ctx context.Context, payload []byte) (any, error) {
	var req executor.Request
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, dispatch.Errorf("invalid_request", "unmarshal stream request: %v", err)
		}
	}
	return v.exec.ExecuteStream(ctx, req)
}

func (v *Venice) handleExecutorCountTokens(ctx context.Context, payload []byte) (any, error) {
	var req executor.Request
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &req)
	}
	return v.exec.CountTokens(ctx, req)
}

func (v *Venice) handleExecutorHTTPRequest(ctx context.Context, payload []byte) (any, error) {
	var req executor.HTTPRequest
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, dispatch.Errorf("invalid_request", "unmarshal http request: %v", err)
		}
	}
	return v.exec.HTTPRequest(ctx, req)
}

func (v *Venice) handleReconfigure(ctx context.Context, payload []byte) (any, error) {
	v.mu.Lock()
	if len(payload) > 0 {
		var cfgMap map[string]any
		if err := json.Unmarshal(payload, &cfgMap); err == nil {
			if b, ok := cfgMap["wallet_enabled"].(bool); ok {
				v.cfg.WalletEnabled = b
			}
			if s, ok := cfgMap["wallet_key_env"].(string); ok && s != "" {
				v.cfg.WalletKeyEnv = s
			}
			if s, ok := cfgMap["base_url"].(string); ok && s != "" {
				v.cfg.BaseURL = s
			}
			if sec, ok := cfgMap["models_cache_seconds"].(float64); ok && sec > 0 {
				v.cfg.ModelsCacheSeconds = int(sec)
			}
		}
	}
	if v.models != nil {
		v.models.SetCacheTTL(v.cfg.ModelsCacheSeconds)
		v.models.Invalidate()
	}
	if v.exec != nil {
		v.exec.UpdateConfig(executor.Config{
			BaseURL:       v.cfg.BaseURL,
			WalletEnabled: v.cfg.WalletEnabled,
			WalletKeyEnv:  v.cfg.WalletKeyEnv,
		})
	}
	v.mu.Unlock()
	return v.handleRegister(ctx, payload)
}

func (v *Venice) handleModelRegister(ctx context.Context, payload []byte) (any, error) {
	var req models.RegistrationRequest
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &req)
	}
	return v.models.RegisterModels(ctx, req)
}

func (v *Venice) handleQuotaIdentifier(context.Context, []byte) (any, error) {
	return identifierResponse{Identifier: ProviderName}, nil
}

func (v *Venice) handleQuotaDescribe(ctx context.Context, payload []byte) (any, error) {
	var req quota.DescribeRequest
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &req)
	}
	return v.quota.Describe(ctx, req)
}

func (v *Venice) handleQuotaFetch(ctx context.Context, payload []byte) (any, error) {
	var req quota.FetchRequest
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, dispatch.Errorf("invalid_request", "unmarshal quota fetch request: %v", err)
		}
	}
	return v.quota.Fetch(ctx, req)
}
