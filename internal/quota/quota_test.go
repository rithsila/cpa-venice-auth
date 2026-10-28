package quota

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQuotaDescribe(t *testing.T) {
	svc := NewService(nil, "")
	resp, err := svc.Describe(context.Background(), DescribeRequest{})
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if len(resp.SupportedProviders) != 1 || resp.SupportedProviders[0] != "venice" {
		t.Errorf("supported providers = %v, want ['venice']", resp.SupportedProviders)
	}
}

func TestQuotaFetchRateLimitsAndBilling(t *testing.T) {
	rateLimitsJSON := `{
		"data": {
			"accessPermitted": true,
			"apiTier": {
				"id": "paid",
				"isCharged": true
			},
			"balances": {
				"USD": 45.50,
				"DIEM": 100.0,
				"BUNDLED_CREDITS": 10.0
			},
			"nextEpochBegins": "2026-11-01T00:00:00.000Z",
			"rateLimits": [
				{
					"apiModelId": "llama-3.3-70b",
					"rateLimits": [
						{"amount": 100, "type": "RPM"},
						{"amount": 50000, "type": "TPM"}
					]
				}
			]
		}
	}`

	billingJSON := `{
		"canConsume": true,
		"consumptionCurrency": "USD",
		"balances": {
			"diem": 100.0,
			"usd": 45.50,
			"bundledCredits": 10.0,
			"earnedCredits": 0.0
		},
		"diemEpochAllocation": 100
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-api-key" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api_keys/rate_limits":
			_, _ = w.Write([]byte(rateLimitsJSON))
		case "/billing/balance":
			_, _ = w.Write([]byte(billingJSON))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	svc := NewService(server.Client(), server.URL)
	storageBytes, _ := json.Marshal(map[string]any{
		"api_key": "test-api-key",
	})

	resp, err := svc.Fetch(context.Background(), FetchRequest{
		AuthID:      "venice-main",
		StorageJSON: storageBytes,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if resp.Subscription.TierID != "paid" {
		t.Errorf("tierId = %q, want paid", resp.Subscription.TierID)
	}

	var foundUSD, foundDIEM bool
	for _, m := range resp.Summary {
		if m.Key == "balance_usd" && m.Value == 45.50 {
			foundUSD = true
		}
		if m.Key == "balance_diem" && m.Value == 100.0 {
			foundDIEM = true
		}
	}
	if !foundUSD {
		t.Errorf("USD balance metric missing or incorrect: %+v", resp.Summary)
	}
	if !foundDIEM {
		t.Errorf("DIEM balance metric missing or incorrect: %+v", resp.Summary)
	}

	if len(resp.Groups) == 0 || len(resp.Groups[0].Buckets) != 2 {
		t.Fatalf("expected rate limit buckets, got: %+v", resp.Groups)
	}
}

func TestQuotaFetchRejectsMissingAPIKey(t *testing.T) {
	svc := NewService(nil, "")
	_, err := svc.Fetch(context.Background(), FetchRequest{
		StorageJSON: []byte(`{}`),
	})
	if err == nil {
		t.Fatal("expected error with empty storage, got nil")
	}
}
