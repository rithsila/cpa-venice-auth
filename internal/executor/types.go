package executor

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
)

// RawBytes unmarshals from base64 string or raw bytes.
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

// Request describes a model execution call.
type Request struct {
	AuthID          string         `json:"AuthID,omitempty"`
	AuthProvider    string         `json:"AuthProvider,omitempty"`
	Model           string         `json:"Model,omitempty"`
	Format          string         `json:"Format,omitempty"`
	Stream          bool           `json:"Stream,omitempty"`
	Alt             string         `json:"Alt,omitempty"`
	Headers         http.Header    `json:"Headers,omitempty"`
	Query           url.Values     `json:"Query,omitempty"`
	OriginalRequest RawBytes       `json:"OriginalRequest,omitempty"`
	SourceFormat    string         `json:"SourceFormat,omitempty"`
	Payload         RawBytes       `json:"Payload,omitempty"`
	Metadata        map[string]any `json:"Metadata,omitempty"`
	StorageJSON     RawBytes       `json:"StorageJSON,omitempty"`
	StreamID        string         `json:"stream_id,omitempty"`
	HostCallbackID  string         `json:"host_callback_id,omitempty"`
}

// Response returns non-streaming execution output.
type Response struct {
	Payload  []byte         `json:"Payload"`
	Headers  http.Header    `json:"Headers,omitempty"`
	Metadata map[string]any `json:"Metadata,omitempty"`
}

// StreamResponse returns streaming execution output.
type StreamResponse struct {
	Headers http.Header   `json:"headers,omitempty"`
	Chunks  []StreamChunk `json:"chunks,omitempty"`
}

// StreamChunk carries one chunk or error.
type StreamChunk struct {
	Payload []byte `json:"Payload"`
	Error   string `json:"Error,omitempty"`
}

// HTTPRequest mirrors ExecutorHTTPRequest.
type HTTPRequest struct {
	Method      string         `json:"Method"`
	URL         string         `json:"URL"`
	Headers     http.Header    `json:"Headers,omitempty"`
	Body        RawBytes       `json:"Body,omitempty"`
	StorageJSON RawBytes       `json:"StorageJSON,omitempty"`
	Metadata    map[string]any `json:"Metadata,omitempty"`
}

// HTTPResponse mirrors ExecutorHTTPResponse.
type HTTPResponse struct {
	StatusCode int         `json:"StatusCode"`
	Headers    http.Header `json:"Headers,omitempty"`
	Body       []byte      `json:"Body,omitempty"`
}

// Config specifies executor options.
type Config struct {
	BaseURL string
}

// DefaultConfig provides defaults for the executor.
func DefaultConfig() Config {
	return Config{
		BaseURL: "https://api.venice.ai/api/v1",
	}
}
