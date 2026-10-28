package dispatch

import (
	"context"
	"testing"

	"cpa-venice-auth/internal/envelope"
)

func decode(t *testing.T, raw []byte) *envelope.Envelope {
	t.Helper()
	env, err := envelope.Decode(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return env
}

func TestUnknownMethod(t *testing.T) {
	d := New()
	raw, ok := d.Call("does.not.exist", nil)
	if ok {
		t.Fatal("ok = true, want false")
	}
	env := decode(t, raw)
	if env.Error == nil || env.Error.Code != "unknown_method" {
		t.Fatalf("error = %+v, want unknown_method", env.Error)
	}
}

func TestFaultCodeIsPreserved(t *testing.T) {
	d := New()
	d.Register("auth.refresh", func(context.Context, []byte) (any, error) {
		return nil, Errorf("auth_invalid", "key rejected")
	})
	raw, ok := d.Call("auth.refresh", nil)
	if ok {
		t.Fatal("ok = true, want false")
	}
	env := decode(t, raw)
	if env.Error == nil || env.Error.Code != "auth_invalid" {
		t.Fatalf("error = %+v, want auth_invalid", env.Error)
	}
	if env.Error.Message != "key rejected" {
		t.Fatalf("message = %q", env.Error.Message)
	}
}

func TestPanicIsContained(t *testing.T) {
	d := New()
	d.Register("boom", func(context.Context, []byte) (any, error) {
		panic("handler exploded")
	})
	raw, ok := d.Call("boom", nil)
	if ok {
		t.Fatal("ok = true, want false")
	}
	env := decode(t, raw)
	if env.Error == nil || env.Error.Code != "plugin_error" {
		t.Fatalf("error = %+v, want plugin_error", env.Error)
	}
}

func TestHandlerSuccess(t *testing.T) {
	d := New()
	d.Register("auth.identifier", func(context.Context, []byte) (any, error) {
		return map[string]string{"identifier": "venice"}, nil
	})
	raw, ok := d.Call("auth.identifier", nil)
	if !ok {
		t.Fatalf("ok = false: %s", raw)
	}
	env := decode(t, raw)
	if !env.OK {
		t.Fatalf("ok field = false: %s", raw)
	}
}
