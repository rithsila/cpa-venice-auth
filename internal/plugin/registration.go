package plugin

// JSON keys in this file are capitalized on purpose. The CLIProxyAPI plugin
// ABI uses Go's default field names for metadata, so "Name" and not "name".
// Capability keys are lowercase snake case.

type configField struct {
	Name        string `json:"Name"`
	Type        string `json:"Type"`
	Default     any    `json:"Default"`
	Description string `json:"Description,omitempty"`
}

type metadata struct {
	Name             string        `json:"Name"`
	Version          string        `json:"Version"`
	Author           string        `json:"Author"`
	GitHubRepository string        `json:"GitHubRepository,omitempty"`
	Logo             string        `json:"Logo,omitempty"`
	ConfigFields     []configField `json:"ConfigFields"`
}

type capabilities struct {
	AuthProvider          bool     `json:"auth_provider"`
	Executor              bool     `json:"executor"`
	ExecutorModelScope    string   `json:"executor_model_scope,omitempty"`
	ExecutorInputFormats  []string `json:"executor_input_formats,omitempty"`
	ExecutorOutputFormats []string `json:"executor_output_formats,omitempty"`
}

type registration struct {
	SchemaVersion uint32       `json:"schema_version"`
	Metadata      metadata     `json:"metadata"`
	Capabilities  capabilities `json:"capabilities"`
}

func buildRegistration() registration {
	return registration{
		SchemaVersion: 1,
		Metadata: metadata{
			Name:             PluginID,
			Version:          Version,
			Author:           "TRISPER",
			GitHubRepository: RepoURL,
			ConfigFields: []configField{
				{
					Name:        "wallet_enabled",
					Type:        "boolean",
					Default:     false,
					Description: "Enable x402 wallet credentials",
				},
				{
					Name:        "models_cache_seconds",
					Type:        "integer",
					Default:     10800,
					Description: "Seconds to cache the Venice model catalog",
				},
				{
					Name:        "wallet_key_env",
					Type:        "string",
					Default:     "VENICE_X402_PRIVATE_KEY",
					Description: "Environment variable holding the wallet private key",
				},
				{
					Name:        "base_url",
					Type:        "string",
					Default:     "https://api.venice.ai/api/v1",
					Description: "Venice API base URL",
				},
			},
		},
		Capabilities: capabilities{
			AuthProvider:          true,
			Executor:              true,
			ExecutorModelScope:    "both",
			ExecutorInputFormats:  []string{"chat-completions"},
			ExecutorOutputFormats: []string{"chat-completions"},
		},
	}
}

type identifierResponse struct {
	Identifier string `json:"identifier"`
}

type authParseResponse struct {
	Handled bool `json:"Handled"`
}
