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
	if !reg.Capabilities.Executor {
		t.Fatal("executor = false, want true in wave 3")
	}
	if reg.Capabilities.ExecutorModelScope != "both" {
		t.Fatalf("executor_model_scope = %q, want both", reg.Capabilities.ExecutorModelScope)
	}
	if len(reg.Capabilities.ExecutorInputFormats) != 1 || reg.Capabilities.ExecutorInputFormats[0] != "chat-completions" {
		t.Fatalf("executor_input_formats = %v, want [chat-completions]", reg.Capabilities.ExecutorInputFormats)
	}
	if len(reg.Capabilities.ExecutorOutputFormats) != 1 || reg.Capabilities.ExecutorOutputFormats[0] != "chat-completions" {
		t.Fatalf("executor_output_formats = %v, want [chat-completions]", reg.Capabilities.ExecutorOutputFormats)
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
	d := newDispatcher()
	reqBytes, _ := json.Marshal(map[string]any{
		"Provider": "openai",
		"RawJSON":  []byte(`{"type":"openai","key":"sk-123"}`),
	})
	raw, ok := d.Call("auth.parse", reqBytes)
	if !ok {
		t.Fatalf("auth.parse call failed: %s", raw)
	}
	env, err := envelope.Decode(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	var res authParseResponse
	if err := json.Unmarshal(env.Result, &res); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Handled {
		t.Fatal("handled = true, want false for non-venice file")
	}
}

func TestAuthParseAcceptsVeniceKey(t *testing.T) {
	d := newDispatcher()
	reqBytes, _ := json.Marshal(map[string]any{
		"FileName": "venice-main.json",
		"RawJSON":  []byte(`{"type":"venice","api_key":"vnc_test_secret"}`),
	})
	raw, ok := d.Call("auth.parse", reqBytes)
	if !ok {
		t.Fatalf("auth.parse failed: %s", raw)
	}
	env, err := envelope.Decode(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	var res struct {
		Handled bool
		Auth    struct {
			Provider string
			ID       string
			FileName string
			Label    string
		}
	}
	if err := json.Unmarshal(env.Result, &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !res.Handled {
		t.Fatal("handled = false, want true")
	}
	if res.Auth.Provider != "venice" {
		t.Fatalf("provider = %q, want venice", res.Auth.Provider)
	}
	if res.Auth.ID != "venice-main" {
		t.Fatalf("id = %q, want venice-main", res.Auth.ID)
	}
}

func TestAuthLoginStartAndPoll(t *testing.T) {
	d := newDispatcher()
	rawStart, ok := d.Call("auth.login.start", []byte(`{"Provider":"venice"}`))
	if !ok {
		t.Fatalf("auth.login.start failed: %s", rawStart)
	}
	envStart, _ := envelope.Decode(rawStart)
	var startRes struct {
		Provider string
		URL      string
		State    string
	}
	_ = json.Unmarshal(envStart.Result, &startRes)
	if startRes.Provider != "venice" || startRes.URL != "https://venice.ai/settings/api" {
		t.Fatalf("unexpected start response: %+v", startRes)
	}

	rawPoll, ok := d.Call("auth.login.poll", []byte(`{"Provider":"venice","State":"`+startRes.State+`"}`))
	if !ok {
		t.Fatalf("auth.login.poll failed: %s", rawPoll)
	}
	envPoll, _ := envelope.Decode(rawPoll)
	var pollRes struct {
		Status  string
		Message string
	}
	_ = json.Unmarshal(envPoll.Result, &pollRes)
	if pollRes.Status != "pending" {
		t.Fatalf("status = %q, want pending", pollRes.Status)
	}
}

func TestAuthRefreshRejectsMissingCredentials(t *testing.T) {
	d := newDispatcher()
	reqBytes, _ := json.Marshal(map[string]any{
		"AuthID":       "venice-empty",
		"AuthProvider": "venice",
		"StorageJSON":  []byte(`{}`),
	})
	raw, ok := d.Call("auth.refresh", reqBytes)
	if ok {
		t.Fatal("ok = true, want false for empty credentials")
	}
	env, err := envelope.Decode(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Error == nil || env.Error.Code != "auth_invalid" {
		t.Fatalf("error = %+v, want auth_invalid", env.Error)
	}
}

func TestExecutorIdentifier(t *testing.T) {
	env := call(t, newDispatcher(), "executor.identifier")
	var res identifierResponse
	if err := json.Unmarshal(env.Result, &res); err != nil {
		t.Fatalf("identifier: %v", err)
	}
	if res.Identifier != ProviderName {
		t.Fatalf("identifier = %q, want %q", res.Identifier, ProviderName)
	}
}

func TestExecutorCountTokensReturnsUnsupported(t *testing.T) {
	d := newDispatcher()
	raw, ok := d.Call("executor.count_tokens", []byte(`{}`))
	if ok {
		t.Fatal("ok = true, want false for count_tokens")
	}
	env, err := envelope.Decode(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Error == nil || env.Error.Code != "unsupported" {
		t.Fatalf("error = %+v, want unsupported", env.Error)
	}
}

func TestUnimplementedMethodIsCleanError(t *testing.T) {
	d := newDispatcher()
	raw, ok := d.Call("models.list", nil)
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
