// Package plugin implements the Venice capability set.
package plugin

import (
	"context"
	"sync"

	"cpa-venice-auth/internal/dispatch"
	"cpa-venice-auth/internal/host"
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
	closed bool
}

// New builds a provider around a host API.
func New(h *host.API) *Venice {
	return &Venice{cfg: DefaultConfig(), host: h}
}

// Register installs every method this build supports. Methods for waves that
// are not implemented yet are deliberately absent, so the host gets a clean
// unknown_method instead of a half-working stub.
func (v *Venice) Register(d *dispatch.Dispatcher) {
	d.Register("plugin.register", v.handleRegister)
	d.Register("plugin.reconfigure", v.handleRegister)
	d.Register("auth.identifier", v.handleAuthIdentifier)
	d.Register("auth.parse", v.handleAuthParse)
}

// Config returns a copy of the active configuration.
func (v *Venice) Config() Config {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.cfg
}

// Close stops background work. No goroutines exist yet; the flag is here so
// later waves have one shutdown point.
func (v *Venice) Close() {
	v.mu.Lock()
	v.closed = true
	v.mu.Unlock()
}

func (v *Venice) handleRegister(context.Context, []byte) (any, error) {
	return buildRegistration(), nil
}

func (v *Venice) handleAuthIdentifier(context.Context, []byte) (any, error) {
	return identifierResponse{Identifier: ProviderName}, nil
}

// handleAuthParse reports "not handled" until the auth wave lands. Returning
// Handled:false is the ABI's way of saying another provider owns this file.
func (v *Venice) handleAuthParse(context.Context, []byte) (any, error) {
	return authParseResponse{Handled: false}, nil
}
