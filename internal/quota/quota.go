package quota

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"cpa-venice-auth/internal/auth"
	"cpa-venice-auth/internal/dispatch"
)

// DescribeRequest carries context for quota.describe.
type DescribeRequest struct {
	Plugin any `json:"plugin,omitempty"`
}

// DescribeResponse describes quota provider capabilities.
type DescribeResponse struct {
	SupportedProviders []string `json:"supported_providers,omitempty"`
	DisplayName        string   `json:"display_name,omitempty"`
	SupportsReset      bool     `json:"supports_reset,omitempty"`
}

// FetchRequest carries context for quota.fetch.
type FetchRequest struct {
	AuthIndex   string            `json:"auth_index"`
	AuthID      string            `json:"auth_id"`
	Provider    string            `json:"provider"`
	StorageJSON json.RawMessage   `json:"storage_json,omitempty"`
	Metadata    map[string]any    `json:"metadata,omitempty"`
	Attributes  map[string]string `json:"attributes,omitempty"`
}

// FetchResponse is the normalized response for CPA management panel.
type FetchResponse struct {
	Subscription       *Subscription `json:"subscription,omitempty"`
	Summary            []Metric      `json:"summary,omitempty"`
	ServerTimeOffsetMs int64         `json:"serverTimeOffsetMs,omitempty"`
	Groups             []Group       `json:"groups,omitempty"`
}

// Subscription carries plan and tier information.
type Subscription struct {
	Plan     string `json:"plan,omitempty"`
	TierName string `json:"tierName,omitempty"`
	TierID   string `json:"tierId,omitempty"`
}

// Metric is a key metric in the summary list.
type Metric struct {
	Key      string  `json:"key"`
	Label    string  `json:"label"`
	Value    float64 `json:"value"`
	Unit     string  `json:"unit,omitempty"`
	Format   string  `json:"format,omitempty"`
	Currency string  `json:"currency,omitempty"`
}

// Group clusters quota buckets.
type Group struct {
	DisplayName string   `json:"displayName,omitempty"`
	Buckets     []Bucket `json:"buckets,omitempty"`
}

// Bucket represents one limit bucket.
type Bucket struct {
	Window            string  `json:"window,omitempty"`
	RemainingFraction float64 `json:"remainingFraction"`
	ResetTime         string  `json:"resetTime,omitempty"`
	Description       string  `json:"description,omitempty"`
}

// Service queries Venice balance and rate limits.
type Service struct {
	client  *http.Client
	baseURL string
}

// NewService creates a quota service.
func NewService(client *http.Client, baseURL string) *Service {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	if baseURL == "" {
		baseURL = "https://api.venice.ai/api/v1"
	}
	return &Service{
		client:  client,
		baseURL: baseURL,
	}
}

// Describe returns quota capabilities.
func (s *Service) Describe(ctx context.Context, _ DescribeRequest) (DescribeResponse, error) {
	return DescribeResponse{
		SupportedProviders: []string{"venice"},
		DisplayName:        "Venice Quota & Balances",
		SupportsReset:      false,
	}, nil
}

// Fetch gathers balance and rate limit information for a credential.
func (s *Service) Fetch(ctx context.Context, req FetchRequest) (FetchResponse, error) {
	var storage auth.Storage
	if len(req.StorageJSON) > 0 {
		_ = json.Unmarshal(req.StorageJSON, &storage)
	}

	apiKey := storage.APIKey
	if apiKey == "" {
		return FetchResponse{}, dispatch.Errorf("auth_invalid", "no api_key found in storage for quota query")
	}

	baseURL := s.baseURL
	if storage.BaseURL != "" {
		baseURL = storage.BaseURL
	}

	rateLimitsResp, errRL := s.fetchRateLimits(ctx, baseURL, apiKey)
	billingResp, _ := s.fetchBillingBalance(ctx, baseURL, apiKey)

	return buildFetchResponse(rateLimitsResp, billingResp), errRL
}

type rateLimitsData struct {
	Data struct {
		AccessPermitted bool `json:"accessPermitted"`
		APITier         struct {
			ID        string `json:"id"`
			IsCharged bool   `json:"isCharged"`
		} `json:"apiTier"`
		Balances struct {
			USD            *float64 `json:"USD"`
			DIEM           *float64 `json:"DIEM"`
			BundledCredits *float64 `json:"BUNDLED_CREDITS"`
		} `json:"balances"`
		NextEpochBegins string `json:"nextEpochBegins"`
		RateLimits      []struct {
			APIModelID string `json:"apiModelId"`
			RateLimits []struct {
				Amount float64 `json:"amount"`
				Type   string  `json:"type"`
			} `json:"rateLimits"`
		} `json:"rateLimits"`
	} `json:"data"`
}

type billingData struct {
	CanConsume          bool   `json:"canConsume"`
	ConsumptionCurrency string `json:"consumptionCurrency"`
	Balances            struct {
		Diem           *float64 `json:"diem"`
		USD            *float64 `json:"usd"`
		BundledCredits *float64 `json:"bundledCredits"`
		EarnedCredits  *float64 `json:"earnedCredits"`
	} `json:"balances"`
	DiemEpochAllocation float64 `json:"diemEpochAllocation"`
}

func (s *Service) fetchRateLimits(ctx context.Context, baseURL, apiKey string) (*rateLimitsData, error) {
	url := strings.TrimRight(baseURL, "/") + "/api_keys/rate_limits"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create rate_limits request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("fetch rate_limits: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, dispatch.Errorf("auth_invalid", "unauthorized: %s", string(body))
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("rate_limits upstream status %d: %s", resp.StatusCode, string(body))
	}

	var data rateLimitsData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode rate_limits response: %w", err)
	}
	return &data, nil
}

func (s *Service) fetchBillingBalance(ctx context.Context, baseURL, apiKey string) (*billingData, error) {
	url := strings.TrimRight(baseURL, "/") + "/billing/balance"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("billing balance status %d", resp.StatusCode)
	}

	var data billingData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return &data, nil
}

func buildFetchResponse(rl *rateLimitsData, billing *billingData) FetchResponse {
	resp := FetchResponse{
		Subscription: &Subscription{
			Plan:     "Pay-as-you-go",
			TierName: "Standard",
			TierID:   "standard",
		},
		Summary: make([]Metric, 0),
		Groups:  make([]Group, 0),
	}

	if rl != nil && rl.Data.APITier.ID != "" {
		resp.Subscription.TierID = rl.Data.APITier.ID
		resp.Subscription.TierName = strings.ToUpper(rl.Data.APITier.ID)
	}

	var usdVal, diemVal float64
	var hasUSD, hasDIEM bool

	if billing != nil {
		if billing.Balances.USD != nil {
			usdVal = *billing.Balances.USD
			hasUSD = true
		}
		if billing.Balances.Diem != nil {
			diemVal = *billing.Balances.Diem
			hasDIEM = true
		}
	} else if rl != nil {
		if rl.Data.Balances.USD != nil {
			usdVal = *rl.Data.Balances.USD
			hasUSD = true
		}
		if rl.Data.Balances.DIEM != nil {
			diemVal = *rl.Data.Balances.DIEM
			hasDIEM = true
		}
	}

	if hasUSD {
		resp.Summary = append(resp.Summary, Metric{
			Key:      "balance_usd",
			Label:    "USD Balance",
			Value:    usdVal,
			Format:   "currency",
			Currency: "USD",
		})
	}
	if hasDIEM {
		resp.Summary = append(resp.Summary, Metric{
			Key:   "balance_diem",
			Label: "DIEM Balance",
			Value: diemVal,
			Unit:  "DIEM",
		})
	}

	if rl != nil && len(rl.Data.RateLimits) > 0 {
		var buckets []Bucket
		for _, modelLim := range rl.Data.RateLimits {
			for _, lim := range modelLim.RateLimits {
				desc := fmt.Sprintf("%s: %.0f %s", modelLim.APIModelID, lim.Amount, lim.Type)
				buckets = append(buckets, Bucket{
					Window:            lim.Type,
					RemainingFraction: 1.0,
					ResetTime:         rl.Data.NextEpochBegins,
					Description:       desc,
				})
			}
		}
		if len(buckets) > 0 {
			resp.Groups = append(resp.Groups, Group{
				DisplayName: "Rate Limits",
				Buckets:     buckets,
			})
		}
	}

	return resp
}
