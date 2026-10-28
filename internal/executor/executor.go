package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"cpa-venice-auth/internal/auth"
	"cpa-venice-auth/internal/dispatch"
	"cpa-venice-auth/internal/host"
	"cpa-venice-auth/internal/x402"
)

// Service executes Venice AI chat completions and generic requests.
type Service struct {
	client       *http.Client
	hostCall     host.CallFunc
	cfg          Config
	balanceGuard *x402.BalanceGuard
	signersMu    sync.RWMutex
	signers      map[string]*x402.Signer
	streamsMu    sync.Mutex
	activeCancel map[string]context.CancelFunc
	streamsWg    sync.WaitGroup
}

// NewService creates an executor service.
func NewService(client *http.Client, hostCall host.CallFunc, cfg Config) *Service {
	if client == nil {
		client = &http.Client{}
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.venice.ai/api/v1"
	}
	if cfg.WalletKeyEnv == "" {
		cfg.WalletKeyEnv = "VENICE_X402_PRIVATE_KEY"
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 2
	}
	return &Service{
		client:       client,
		hostCall:     hostCall,
		cfg:          cfg,
		balanceGuard: x402.NewBalanceGuard(),
		signers:      make(map[string]*x402.Signer),
		activeCancel: make(map[string]context.CancelFunc),
	}
}

// UpdateConfig refreshes runtime configuration parameters.
func (s *Service) UpdateConfig(cfg Config) {
	s.signersMu.Lock()
	defer s.signersMu.Unlock()
	if cfg.BaseURL != "" {
		s.cfg.BaseURL = cfg.BaseURL
	}
	s.cfg.WalletEnabled = cfg.WalletEnabled
	if cfg.WalletKeyEnv != "" {
		s.cfg.WalletKeyEnv = cfg.WalletKeyEnv
	}
	if cfg.MaxRetries > 0 {
		s.cfg.MaxRetries = cfg.MaxRetries
	}
}

// Close cancels all in-flight streams and waits up to timeout for them to finish.
func (s *Service) Close(timeout time.Duration) {
	s.streamsMu.Lock()
	for _, cancel := range s.activeCancel {
		cancel()
	}
	s.streamsMu.Unlock()

	c := make(chan struct{})
	go func() {
		defer close(c)
		s.streamsWg.Wait()
	}()

	select {
	case <-c:
	case <-time.After(timeout):
	}
}

// Balance returns the current cached balance and consumption status.
func (s *Service) Balance() (balanceUSD float64, canConsume bool, checked bool) {
	if s.balanceGuard != nil {
		return s.balanceGuard.Balance()
	}
	return 0, false, false
}

func (s *Service) isWalletMode(storage auth.Storage) bool {
	if storage.Mode == "wallet" {
		return true
	}
	if storage.APIKey == "" && (storage.WalletKey != "" || (s.cfg.WalletEnabled && os.Getenv(s.cfg.WalletKeyEnv) != "")) {
		return true
	}
	return false
}

func (s *Service) getWalletKey(storage auth.Storage) string {
	if storage.WalletKey != "" {
		return storage.WalletKey
	}
	if s.cfg.WalletKeyEnv != "" {
		return os.Getenv(s.cfg.WalletKeyEnv)
	}
	return ""
}

func (s *Service) getSigner(walletKeyHex string) (*x402.Signer, error) {
	walletKeyHex = strings.TrimSpace(walletKeyHex)
	if walletKeyHex == "" {
		return nil, fmt.Errorf("x402 wallet key missing: neither storage.wallet_key nor %s env var is set", s.cfg.WalletKeyEnv)
	}

	s.signersMu.RLock()
	signer, ok := s.signers[walletKeyHex]
	s.signersMu.RUnlock()
	if ok {
		return signer, nil
	}

	s.signersMu.Lock()
	defer s.signersMu.Unlock()
	if signer, ok := s.signers[walletKeyHex]; ok {
		return signer, nil
	}

	signer, err := x402.NewSigner(walletKeyHex)
	if err != nil {
		return nil, fmt.Errorf("init x402 signer: %w", err)
	}
	s.signers[walletKeyHex] = signer
	return signer, nil
}

// Execute performs non-streaming model completion.
func (s *Service) Execute(ctx context.Context, req Request) (Response, error) {
	storage := s.extractStorage(req.StorageJSON)
	var signer *x402.Signer
	if s.isWalletMode(storage) {
		walletKey := s.getWalletKey(storage)
		sig, err := s.getSigner(walletKey)
		if err != nil {
			return Response{}, dispatch.ErrorWithStatus("auth_invalid", http.StatusUnauthorized, 0, "%v", err)
		}
		signer = sig

		baseURL := s.cfg.BaseURL
		if storage.BaseURL != "" {
			baseURL = storage.BaseURL
		}
		if err := s.balanceGuard.EnsureBalance(ctx, s.client, baseURL, signer); err != nil {
			return Response{}, dispatch.ErrorWithStatus("insufficient_balance", http.StatusPaymentRequired, 0, "%v", err)
		}
	}

	var lastErr error
	maxAttempts := 1 + s.cfg.MaxRetries
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return Response{}, ctx.Err()
			case <-time.After(time.Duration(attempt) * 100 * time.Millisecond):
			}
		}

		httpReq, err := s.buildHTTPRequest(ctx, req, false)
		if err != nil {
			return Response{}, err
		}

		resp, err := s.client.Do(httpReq)
		if err != nil {
			lastErr = dispatch.ErrorWithStatus("upstream_error", http.StatusBadGateway, 0, "execute request: %v", err)
			continue
		}

		if date := resp.Header.Get("Date"); date != "" && signer != nil {
			_ = signer.UpdateClockSkew(date)
		}
		if s.balanceGuard != nil {
			s.balanceGuard.UpdateFromHeader(resp.Header)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = dispatch.ErrorWithStatus("upstream_error", http.StatusBadGateway, 0, "read response body: %v", err)
			continue
		}

		if resp.StatusCode >= 500 && resp.StatusCode <= 504 && attempt < maxAttempts-1 {
			lastErr = s.mapError(resp.StatusCode, resp.Header, body)
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return Response{}, s.mapError(resp.StatusCode, resp.Header, body)
		}

		headers := make(http.Header)
		for k, v := range resp.Header {
			headers[k] = append([]string(nil), v...)
		}

		return Response{
			Payload: body,
			Headers: headers,
		}, nil
	}

	return Response{}, lastErr
}

// ExecuteStream performs streaming model completion.
func (s *Service) ExecuteStream(ctx context.Context, req Request) (StreamResponse, error) {
	storage := s.extractStorage(req.StorageJSON)
	var signer *x402.Signer
	if s.isWalletMode(storage) {
		walletKey := s.getWalletKey(storage)
		sig, err := s.getSigner(walletKey)
		if err != nil {
			return StreamResponse{}, dispatch.ErrorWithStatus("auth_invalid", http.StatusUnauthorized, 0, "%v", err)
		}
		signer = sig

		baseURL := s.cfg.BaseURL
		if storage.BaseURL != "" {
			baseURL = storage.BaseURL
		}
		if err := s.balanceGuard.EnsureBalance(ctx, s.client, baseURL, signer); err != nil {
			return StreamResponse{}, dispatch.ErrorWithStatus("insufficient_balance", http.StatusPaymentRequired, 0, "%v", err)
		}
	}

	httpReq, err := s.buildHTTPRequest(ctx, req, true)
	if err != nil {
		return StreamResponse{}, err
	}

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return StreamResponse{}, dispatch.ErrorWithStatus("upstream_error", http.StatusBadGateway, 0, "execute stream request: %v", err)
	}

	if date := resp.Header.Get("Date"); date != "" && signer != nil {
		_ = signer.UpdateClockSkew(date)
	}
	if s.balanceGuard != nil {
		s.balanceGuard.UpdateFromHeader(resp.Header)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return StreamResponse{}, s.mapError(resp.StatusCode, resp.Header, body)
	}

	streamHeaders := make(http.Header)
	for k, v := range resp.Header {
		streamHeaders[k] = append([]string(nil), v...)
	}
	if streamHeaders.Get("Content-Type") == "" {
		streamHeaders.Set("Content-Type", "text/event-stream")
	}

	// If a StreamID was passed and hostCall is available, forward chunks asynchronously
	if req.StreamID != "" && s.hostCall != nil {
		streamCtx, cancel := context.WithCancel(ctx)
		s.streamsMu.Lock()
		s.activeCancel[req.StreamID] = cancel
		s.streamsMu.Unlock()

		s.streamsWg.Add(1)
		go func() {
			defer s.streamsWg.Done()
			s.forwardStreamToHost(streamCtx, req.StreamID, resp.Body)
			s.streamsMu.Lock()
			delete(s.activeCancel, req.StreamID)
			s.streamsMu.Unlock()
			cancel()
		}()
		return StreamResponse{Headers: streamHeaders}, nil
	}

	// Fallback for tests or direct calls without host stream bridge
	chunks, err := s.readAllChunks(resp.Body)
	if err != nil {
		return StreamResponse{}, err
	}
	return StreamResponse{
		Headers: streamHeaders,
		Chunks:  chunks,
	}, nil
}

// CountTokens is unsupported on Venice (Venice does not have a public count_tokens endpoint).
func (s *Service) CountTokens(ctx context.Context, req Request) (Response, error) {
	return Response{}, dispatch.Errorf("unsupported", "count_tokens is unsupported on Venice provider")
}

// HTTPRequest handles generic HTTP proxying through the executor.
func (s *Service) HTTPRequest(ctx context.Context, req HTTPRequest) (HTTPResponse, error) {
	targetURL := req.URL
	if targetURL == "" {
		return HTTPResponse{}, dispatch.Errorf("invalid_request", "URL is required")
	}

	method := req.Method
	if method == "" {
		method = http.MethodGet
	}

	var bodyReader io.Reader
	if len(req.Body) > 0 {
		bodyReader = bytes.NewReader(req.Body)
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, targetURL, bodyReader)
	if err != nil {
		return HTTPResponse{}, fmt.Errorf("create http request: %w", err)
	}

	for k, vals := range req.Headers {
		for _, v := range vals {
			httpReq.Header.Add(k, v)
		}
	}

	storage := s.extractStorage(req.StorageJSON)
	var signer *x402.Signer
	if s.isWalletMode(storage) {
		walletKey := s.getWalletKey(storage)
		if sig, err := s.getSigner(walletKey); err == nil {
			signer = sig
			if siwx, err := signer.GenerateHeader(); err == nil {
				httpReq.Header.Set("SIGN-IN-WITH-X", siwx)
			}
		}
	} else {
		apiKey := s.extractAPIKey(req.StorageJSON)
		if apiKey != "" && httpReq.Header.Get("Authorization") == "" {
			httpReq.Header.Set("Authorization", "Bearer "+apiKey)
		}
	}

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return HTTPResponse{}, dispatch.ErrorWithStatus("upstream_error", http.StatusBadGateway, 0, "do request: %v", err)
	}
	defer resp.Body.Close()

	if date := resp.Header.Get("Date"); date != "" && signer != nil {
		_ = signer.UpdateClockSkew(date)
	}
	if s.balanceGuard != nil {
		s.balanceGuard.UpdateFromHeader(resp.Header)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return HTTPResponse{}, dispatch.ErrorWithStatus("upstream_error", http.StatusBadGateway, 0, "read response: %v", err)
	}

	return HTTPResponse{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Body:       respBody,
	}, nil
}

func (s *Service) buildHTTPRequest(ctx context.Context, req Request, stream bool) (*http.Request, error) {
	baseURL := s.cfg.BaseURL
	storage := s.extractStorage(req.StorageJSON)
	if storage.BaseURL != "" {
		baseURL = storage.BaseURL
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/chat/completions"

	var bodyBytes []byte
	if len(req.Payload) > 0 {
		bodyBytes = req.Payload
	} else if len(req.OriginalRequest) > 0 {
		bodyBytes = req.OriginalRequest
	}

	if stream && len(bodyBytes) > 0 {
		var payloadMap map[string]any
		if err := json.Unmarshal(bodyBytes, &payloadMap); err == nil {
			if isStream, ok := payloadMap["stream"].(bool); !ok || !isStream {
				payloadMap["stream"] = true
				if updated, err := json.Marshal(payloadMap); err == nil {
					bodyBytes = updated
				}
			}
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	for k, vals := range req.Headers {
		if strings.EqualFold(k, "Authorization") || strings.EqualFold(k, "Host") || strings.EqualFold(k, "Content-Length") {
			continue
		}
		for _, v := range vals {
			httpReq.Header.Add(k, v)
		}
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if stream {
		httpReq.Header.Set("Accept", "text/event-stream")
	} else {
		httpReq.Header.Set("Accept", "application/json")
	}

	if s.isWalletMode(storage) {
		walletKey := s.getWalletKey(storage)
		signer, err := s.getSigner(walletKey)
		if err != nil {
			return nil, err
		}
		siwxHeader, err := signer.GenerateHeader()
		if err != nil {
			return nil, fmt.Errorf("generate x402 header: %w", err)
		}
		httpReq.Header.Set("SIGN-IN-WITH-X", siwxHeader)
	} else if storage.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+storage.APIKey)
	}

	return httpReq, nil
}

func (s *Service) forwardStreamToHost(ctx context.Context, streamID string, r io.ReadCloser) {
	defer r.Close()
	go func() {
		<-ctx.Done()
		_ = r.Close()
	}()
	reader := bufio.NewReader(r)

	type rpcStreamEmitRequest struct {
		StreamID string `json:"stream_id"`
		Payload  []byte `json:"payload,omitempty"`
		Error    string `json:"error,omitempty"`
	}
	type rpcStreamCloseRequest struct {
		StreamID string `json:"stream_id"`
		Error    string `json:"error,omitempty"`
	}

	buf := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			closeReq := rpcStreamCloseRequest{StreamID: streamID, Error: "stream canceled"}
			payload, _ := json.Marshal(closeReq)
			_, _ = s.hostCall("host.stream.close", payload)
			return
		default:
		}

		n, err := reader.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			payload, _ := json.Marshal(rpcStreamEmitRequest{
				StreamID: streamID,
				Payload:  chunk,
			})
			_, _ = s.hostCall("host.stream.emit", payload)
		}
		if err != nil {
			closeReq := rpcStreamCloseRequest{StreamID: streamID}
			if err != io.EOF {
				closeReq.Error = err.Error()
			}
			payload, _ := json.Marshal(closeReq)
			_, _ = s.hostCall("host.stream.close", payload)
			return
		}
	}
}

func (s *Service) readAllChunks(r io.ReadCloser) ([]StreamChunk, error) {
	defer r.Close()
	reader := bufio.NewReader(r)
	var chunks []StreamChunk
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			chunks = append(chunks, StreamChunk{
				Payload: append([]byte(nil), buf[:n]...),
			})
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return chunks, err
		}
	}
	return chunks, nil
}

func (s *Service) extractStorage(storageJSON []byte) auth.Storage {
	var storage auth.Storage
	if len(storageJSON) > 0 {
		_ = json.Unmarshal(storageJSON, &storage)
	}
	return storage
}

func (s *Service) extractAPIKey(storageJSON []byte) string {
	storage := s.extractStorage(storageJSON)
	return storage.APIKey
}

func (s *Service) mapError(status int, headers http.Header, body []byte) *dispatch.Fault {
	bodyStr := strings.TrimSpace(string(body))

	retryAfter := 0
	if ra := headers.Get("Retry-After"); ra != "" {
		if sec, err := strconv.Atoi(ra); err == nil {
			retryAfter = sec
		}
	} else if rl := headers.Get("x-ratelimit-reset-requests"); rl != "" {
		if sec, err := strconv.Atoi(rl); err == nil {
			retryAfter = sec
		}
	}

	// 402 Insufficient Balance
	if status == http.StatusPaymentRequired || strings.Contains(bodyStr, "INSUFFICIENT_BALANCE") {
		return dispatch.ErrorWithStatus("insufficient_balance", http.StatusPaymentRequired, 0, "insufficient balance: Venice account has no remaining credits")
	}

	// 401 Pro Only Model
	if strings.Contains(bodyStr, "PRO_ONLY_MODEL") {
		return dispatch.ErrorWithStatus("model_unavailable", http.StatusUnauthorized, 0, "model requires Venice Pro subscription")
	}

	// 401 Invalid Key
	if status == http.StatusUnauthorized {
		return dispatch.ErrorWithStatus("auth_invalid", http.StatusUnauthorized, 0, "invalid Venice API key: %s", bodyStr)
	}

	// 429 Rate Limit
	if status == http.StatusTooManyRequests {
		return dispatch.ErrorWithStatus("rate_limited", http.StatusTooManyRequests, retryAfter, "rate limit reached: %s", bodyStr)
	}

	// Upstream server errors
	if status >= 500 {
		return dispatch.ErrorWithStatus("upstream_error", status, 0, "venice upstream server error (%d): %s", status, bodyStr)
	}

	return dispatch.ErrorWithStatus("plugin_error", status, 0, "venice request failed (%d): %s", status, bodyStr)
}
