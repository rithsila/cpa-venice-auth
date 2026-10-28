// Package host wraps the callbacks CLIProxyAPI exposes to a plugin.
package host

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
)

// CallFunc performs one host callback.
type CallFunc func(method string, payload []byte) ([]byte, error)

// API is the plugin's view of the host.
type API struct {
	call CallFunc
}

// New wraps a callback function.
func New(call CallFunc) *API {
	return &API{call: call}
}

var redactions = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(Bearer\s+)[A-Za-z0-9._~+/=-]+`),
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{8,}`),
	regexp.MustCompile(`0x[0-9a-fA-F]{64}`),
	regexp.MustCompile(`(?i)(SIGN-IN-WITH-X:\s*)\S+`),
}

// Redact removes credentials from a log line. It runs at the call site so a
// secret is never formatted into a string that later reaches a sink.
func Redact(s string) string {
	for _, re := range redactions {
		s = re.ReplaceAllString(s, "${1}REDACTED")
	}
	return s
}

type logRequest struct {
	Level   string         `json:"level,omitempty"`
	Message string         `json:"message,omitempty"`
	Fields  map[string]any `json:"fields,omitempty"`
}

// Log writes to the host log. If the host has no log callback it falls back
// to stderr so the message is not lost.
func (a *API) Log(level, format string, args ...any) {
	message := Redact(fmt.Sprintf(format, args...))
	if a != nil && a.call != nil {
		payload, err := json.Marshal(logRequest{Level: level, Message: message})
		if err == nil {
			if _, err = a.call("host.log", payload); err == nil {
				return
			}
		}
	}
	log.Printf("[%s] %s", level, message)
}

// Call runs a host callback and returns the raw reply.
func (a *API) Call(method string, payload []byte) ([]byte, error) {
	if a == nil || a.call == nil {
		return nil, fmt.Errorf("host callback unavailable: %s", method)
	}
	return a.call(method, payload)
}
