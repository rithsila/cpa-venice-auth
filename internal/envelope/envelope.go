// Package envelope implements the JSON envelope used by the CLIProxyAPI
// plugin ABI. Every plugin call returns exactly one envelope.
package envelope

import (
	"encoding/json"
	"fmt"
)

// Error is the machine-readable failure carried by an envelope.
type Error struct {
	Code              string `json:"code"`
	Message           string `json:"message"`
	HTTPStatus        int    `json:"http_status,omitempty"`
	RetryAfterSeconds int    `json:"retry_after_seconds,omitempty"`
}

// Envelope is the wire format for every plugin response.
type Envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

// OK wraps a result value in a successful envelope.
func OK(result any) ([]byte, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("encode result: %w", err)
	}
	return json.Marshal(Envelope{OK: true, Result: raw})
}

// Fail builds a failure envelope. It never returns an error: a failure
// message must always be deliverable to the host.
func Fail(code, message string) []byte {
	return FailWithDetail(code, message, 0, 0)
}

// FailWithDetail builds a failure envelope with HTTP status and retry details.
func FailWithDetail(code, message string, httpStatus, retryAfter int) []byte {
	raw, _ := json.Marshal(Envelope{
		OK: false,
		Error: &Error{
			Code:              code,
			Message:           message,
			HTTPStatus:        httpStatus,
			RetryAfterSeconds: retryAfter,
		},
	})
	return raw
}

// Decode parses an envelope. Used by tests and by the host callback client
// when it needs to inspect a reply.
func Decode(raw []byte) (*Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("decode envelope: %w", err)
	}
	return &env, nil
}
