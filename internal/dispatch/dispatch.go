// Package dispatch maps plugin method names to handlers and guarantees that
// every call produces an envelope, even when a handler panics.
package dispatch

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"sync"

	"cpa-venice-auth/internal/envelope"
)

// Handler answers one plugin method.
type Handler func(ctx context.Context, payload []byte) (any, error)

// Fault is an error carrying an explicit envelope code and optional HTTP details.
type Fault struct {
	Code            string
	Message         string
	HTTPStatus      int
	RetryAfterSec   int
}

func (f *Fault) Error() string             { return f.Message }
func (f *Fault) FaultCode() string         { return f.Code }
func (f *Fault) StatusCode() int           { return f.HTTPStatus }
func (f *Fault) RetryAfter() int           { return f.RetryAfterSec }
func (f *Fault) RetryAfterSeconds() int    { return f.RetryAfterSec }

// Errorf builds a Fault.
func Errorf(code, format string, args ...any) *Fault {
	return &Fault{Code: code, Message: fmt.Sprintf(format, args...)}
}

// ErrorWithStatus builds a Fault with an HTTP status and optional retry after seconds.
func ErrorWithStatus(code string, httpStatus int, retryAfter int, format string, args ...any) *Fault {
	return &Fault{
		Code:          code,
		Message:       fmt.Sprintf(format, args...),
		HTTPStatus:    httpStatus,
		RetryAfterSec: retryAfter,
	}
}

// Dispatcher holds the method table.
type Dispatcher struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

// New returns an empty dispatcher.
func New() *Dispatcher {
	return &Dispatcher{handlers: make(map[string]Handler)}
}

// Register binds a method name to a handler. Registering twice replaces the
// previous handler; the last registration wins.
func (d *Dispatcher) Register(method string, h Handler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers[method] = h
}

// Call runs one method and always returns an envelope plus a success flag.
// A panic inside a handler is contained here; the host process never sees it.
func (d *Dispatcher) Call(method string, payload []byte) (out []byte, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("venice-provider: panic in %s: %v\n%s", method, r, debug.Stack())
			out, ok = envelope.Fail("plugin_error", fmt.Sprintf("panic in %s: %v", method, r)), false
		}
	}()

	d.mu.RLock()
	h, exists := d.handlers[method]
	d.mu.RUnlock()

	if !exists {
		return envelope.Fail("unknown_method", "unknown method: "+method), false
	}

	result, err := h(context.Background(), payload)
	if err != nil {
		code := "plugin_error"
		status := 0
		retryAfter := 0
		var fault *Fault
		if errors.As(err, &fault) && fault != nil {
			if fault.Code != "" {
				code = fault.Code
			}
			status = fault.HTTPStatus
			retryAfter = fault.RetryAfterSec
		}
		return envelope.FailWithDetail(code, err.Error(), status, retryAfter), false
	}

	raw, err := envelope.OK(result)
	if err != nil {
		return envelope.Fail("plugin_error", err.Error()), false
	}
	return raw, true
}
