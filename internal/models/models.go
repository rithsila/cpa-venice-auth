package models

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ModelInfo describes a model contributed by the plugin.
type ModelInfo struct {
	ID                         string           `json:"ID"`
	Object                     string           `json:"Object"`
	Created                    int64            `json:"Created,omitempty"`
	OwnedBy                    string           `json:"OwnedBy,omitempty"`
	Type                       string           `json:"Type,omitempty"`
	DisplayName                string           `json:"DisplayName,omitempty"`
	Name                       string           `json:"Name,omitempty"`
	Version                    string           `json:"Version,omitempty"`
	Description                string           `json:"Description,omitempty"`
	InputTokenLimit            int64            `json:"InputTokenLimit,omitempty"`
	OutputTokenLimit           int64            `json:"OutputTokenLimit,omitempty"`
	SupportedGenerationMethods []string         `json:"SupportedGenerationMethods,omitempty"`
	ContextLength              int64            `json:"ContextLength,omitempty"`
	MaxCompletionTokens        int64            `json:"MaxCompletionTokens,omitempty"`
	SupportedParameters        []string         `json:"SupportedParameters,omitempty"`
	SupportedInputModalities   []string         `json:"SupportedInputModalities,omitempty"`
	SupportedOutputModalities  []string         `json:"SupportedOutputModalities,omitempty"`
	Thinking                   *ThinkingSupport `json:"Thinking,omitempty"`
	UserDefined                bool             `json:"UserDefined,omitempty"`
}

// ThinkingSupport describes reasoning budget controls.
type ThinkingSupport struct {
	Min            int      `json:"Min,omitempty"`
	Max            int      `json:"Max,omitempty"`
	ZeroAllowed    bool     `json:"ZeroAllowed,omitempty"`
	DynamicAllowed bool     `json:"DynamicAllowed,omitempty"`
	DisableType    string   `json:"DisableType,omitempty"`
	Levels         []string `json:"Levels,omitempty"`
}

// RegistrationRequest carries host registration context.
type RegistrationRequest struct {
	Plugin any `json:"Plugin,omitempty"`
}

// RegistrationResponse is returned to CPA on model.register.
type RegistrationResponse struct {
	Provider string      `json:"Provider"`
	Models   []ModelInfo `json:"Models"`
}

// Service manages the Venice model catalog with in-memory caching.
type Service struct {
	client       *http.Client
	baseURL      string
	cacheTTL     time.Duration
	mu           sync.RWMutex
	cachedModels []ModelInfo
	cachedAt     time.Time
}

// NewService creates a model registrar service.
func NewService(client *http.Client, baseURL string, cacheTTLSeconds int) *Service {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	if baseURL == "" {
		baseURL = "https://api.venice.ai/api/v1"
	}
	if cacheTTLSeconds <= 0 {
		cacheTTLSeconds = 10800
	}
	return &Service{
		client:   client,
		baseURL:  baseURL,
		cacheTTL: time.Duration(cacheTTLSeconds) * time.Second,
	}
}

// SetCacheTTL updates the cache TTL.
func (s *Service) SetCacheTTL(ttlSeconds int) {
	if ttlSeconds <= 0 {
		ttlSeconds = 10800
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cacheTTL = time.Duration(ttlSeconds) * time.Second
}

// Invalidate clears the cache, forcing the next call to fetch fresh models.
func (s *Service) Invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cachedModels = nil
	s.cachedAt = time.Time{}
}

// RegisterModels implements model.register for the plugin host.
func (s *Service) RegisterModels(ctx context.Context, _ RegistrationRequest) (RegistrationResponse, error) {
	s.mu.RLock()
	cached := s.cachedModels
	cachedAt := s.cachedAt
	ttl := s.cacheTTL
	s.mu.RUnlock()

	if len(cached) > 0 && time.Since(cachedAt) < ttl {
		return RegistrationResponse{
			Provider: "venice",
			Models:   append([]ModelInfo(nil), cached...),
		}, nil
	}

	models, err := s.fetchUpstreamModels(ctx)
	if err != nil {
		// If refresh fails and we have stale cache, serve stale cache instead of failing
		if len(cached) > 0 {
			return RegistrationResponse{
				Provider: "venice",
				Models:   append([]ModelInfo(nil), cached...),
			}, nil
		}
		return RegistrationResponse{}, err
	}

	s.mu.Lock()
	s.cachedModels = models
	s.cachedAt = time.Now()
	s.mu.Unlock()

	return RegistrationResponse{
		Provider: "venice",
		Models:   append([]ModelInfo(nil), models...),
	}, nil
}

type veniceModelResponse struct {
	Data []veniceRawModel `json:"data"`
}

type veniceRawModel struct {
	ID            string         `json:"id"`
	Object        string         `json:"object"`
	Created       int64          `json:"created"`
	OwnedBy       string         `json:"owned_by"`
	Type          string         `json:"type"`
	ContextLength int64          `json:"context_length"`
	ModelSpec     veniceRawSpec  `json:"model_spec"`
}

type veniceRawSpec struct {
	Name                   string                 `json:"name"`
	Description            string                 `json:"description"`
	AvailableContextTokens int64                  `json:"availableContextTokens"`
	MaxCompletionTokens    int64                  `json:"maxCompletionTokens"`
	Capabilities           map[string]any         `json:"capabilities"`
}

func (s *Service) fetchUpstreamModels(ctx context.Context) ([]ModelInfo, error) {
	url := strings.TrimRight(s.baseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create models request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("upstream models status %d: %s", resp.StatusCode, string(body))
	}

	var raw veniceModelResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode models: %w", err)
	}

	return ParseVeniceModels(raw.Data), nil
}

// ParseVeniceModels maps raw Venice API model representations into ModelInfo items.
func ParseVeniceModels(items []veniceRawModel) []ModelInfo {
	models := make([]ModelInfo, 0, len(items))
	for _, m := range items {
		if strings.TrimSpace(m.ID) == "" {
			continue
		}

		info := ModelInfo{
			ID:                         m.ID,
			Object:                     m.Object,
			Created:                    m.Created,
			OwnedBy:                    m.OwnedBy,
			Type:                       m.Type,
			DisplayName:                m.ModelSpec.Name,
			Name:                       m.ModelSpec.Name,
			Description:                m.ModelSpec.Description,
			ContextLength:              m.ContextLength,
			InputTokenLimit:            m.ModelSpec.AvailableContextTokens,
			MaxCompletionTokens:        m.ModelSpec.MaxCompletionTokens,
			OutputTokenLimit:           m.ModelSpec.MaxCompletionTokens,
			SupportedGenerationMethods: []string{"chat"},
			SupportedParameters:        []string{"temperature", "top_p", "max_tokens", "stream", "venice_parameters"},
			SupportedInputModalities:   []string{"text"},
			SupportedOutputModalities:  []string{"text"},
		}

		if info.Object == "" {
			info.Object = "model"
		}
		if info.OwnedBy == "" {
			info.OwnedBy = "venice.ai"
		}
		if info.DisplayName == "" {
			info.DisplayName = m.ID
		}
		if info.ContextLength == 0 && m.ModelSpec.AvailableContextTokens > 0 {
			info.ContextLength = m.ModelSpec.AvailableContextTokens
		}
		if info.InputTokenLimit == 0 && info.ContextLength > 0 {
			info.InputTokenLimit = info.ContextLength
		}

		caps := m.ModelSpec.Capabilities
		if caps != nil {
			if v, ok := caps["supportsVision"].(bool); ok && v {
				info.SupportedInputModalities = append(info.SupportedInputModalities, "image")
			}
			if v, ok := caps["supportsAudioInput"].(bool); ok && v {
				info.SupportedInputModalities = append(info.SupportedInputModalities, "audio")
			}
			if v, ok := caps["supportsVideoInput"].(bool); ok && v {
				info.SupportedInputModalities = append(info.SupportedInputModalities, "video")
			}
			if v, ok := caps["supportsFunctionCalling"].(bool); ok && v {
				info.SupportedParameters = append(info.SupportedParameters, "tools", "tool_choice")
			}
			if v, ok := caps["supportsReasoning"].(bool); ok && v {
				ts := &ThinkingSupport{
					ZeroAllowed: true,
				}
				if options, ok := caps["reasoningEffortOptions"].([]any); ok && len(options) > 0 {
					for _, opt := range options {
						if s, ok := opt.(string); ok {
							ts.Levels = append(ts.Levels, s)
						}
					}
				}
				info.Thinking = ts
			}
		}

		models = append(models, info)
	}
	return models
}
