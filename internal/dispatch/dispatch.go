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

// Fault is an error carrying an explicit envelope code.
type Fault struct {
	Code    string
	Message string
}

func (f *Fault) Error() string { return f.Message }

// Errorf builds a Fault.
func Errorf(code, format string, args ...any) error {
	return &Fault{Code: code, Message: fmt.Sprintf(format, args...)}
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
		var fault *Fault
		if errors.As(err, &fault) && fault.Code != "" {
			code = fault.Code
		}
		return envelope.Fail(code, err.Error()), false
	}

	raw, err := envelope.OK(result)
	if err != nil {
		return envelope.Fail("plugin_error", err.Error()), false
	}
	return raw, true
}
