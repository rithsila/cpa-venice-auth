package plugin

import (
	"encoding/json"
	"strings"
	"testing"

	"cpa-venice-auth/internal/dispatch"
	"cpa-venice-auth/internal/envelope"
	"cpa-venice-auth/internal/host"
)

func newDispatcher() *dispatch.Dispatcher {
	d := dispatch.New()
	New(host.New(nil)).Register(d)
	return d
}

func call(t *testing.T, d *dispatch.Dispatcher, method string) *envelope.Envelope {
	t.Helper()
	raw, ok := d.Call(method, nil)
	if !ok {
		t.Fatalf("%s failed: %s", method, raw)
	}
	env, err := envelope.Decode(raw)
	if err != nil {
		t.Fatalf("decode %s: %v", method, err)
	}
	return env
}

func TestRegistration(t *testing.T) {
	env := call(t, newDispatcher(), "plugin.register")
	var reg registration
	if err := json.Unmarshal(env.Result, &reg); err != nil {
		t.Fatalf("registration: %v", err)
	}
	if reg.SchemaVersion != 1 {
		t.Fatalf("schema_version = %d, want 1", reg.SchemaVersion)
	}
	if reg.Metadata.Name != PluginID {
		t.Fatalf("name = %q, want %q", reg.Metadata.Name, PluginID)
	}
	if reg.Metadata.Version != Version {
		t.Fatalf("version = %q, want %q", reg.Metadata.Version, Version)
	}
	if !reg.Capabilities.AuthProvider {
		t.Fatal("auth_provider = false, want true")
	}
	if reg.Capabilities.Executor {
		t.Fatal("executor = true, want false in wave 0")
	}
	if len(reg.Metadata.ConfigFields) != 4 {
		t.Fatalf("config fields = %d, want 4", len(reg.Metadata.ConfigFields))
	}
}

// TestRegistrationSatisfiesHostValidation mirrors validPlugin() in
// internal/pluginhost/host.go. The host rejects a registration whose metadata
// is missing any of these four fields, and logs the misleading message
// "returned invalid metadata or no capabilities".
func TestRegistrationSatisfiesHostValidation(t *testing.T) {
	env := call(t, newDispatcher(), "plugin.register")
	var reg registration
	if err := json.Unmarshal(env.Result, &reg); err != nil {
		t.Fatalf("registration: %v", err)
	}
	meta := reg.Metadata
	for name, value := range map[string]string{
		"Name":             meta.Name,
		"Version":          meta.Version,
		"Author":           meta.Author,
		"GitHubRepository": meta.GitHubRepository,
	} {
		if strings.TrimSpace(value) == "" {
			t.Fatalf("metadata.%s is empty; the host rejects the registration", name)
		}
	}
}

func TestAuthIdentifier(t *testing.T) {
	env := call(t, newDispatcher(), "auth.identifier")
	var res identifierResponse
	if err := json.Unmarshal(env.Result, &res); err != nil {
		t.Fatalf("identifier: %v", err)
	}
	if res.Identifier != ProviderName {
		t.Fatalf("identifier = %q, want %q", res.Identifier, ProviderName)
	}
}

func TestAuthParseDeclinesForeignFiles(t *testing.T) {
	env := call(t, newDispatcher(), "auth.parse")
	var res authParseResponse
	if err := json.Unmarshal(env.Result, &res); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Handled {
		t.Fatal("handled = true, want false until the auth wave")
	}
}

func TestUnimplementedMethodIsCleanError(t *testing.T) {
	d := newDispatcher()
	raw, ok := d.Call("executor.execute", nil)
	if ok {
		t.Fatal("ok = true, want false")
	}
	env, err := envelope.Decode(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Error == nil || env.Error.Code != "unknown_method" {
		t.Fatalf("error = %+v, want unknown_method", env.Error)
	}
}
