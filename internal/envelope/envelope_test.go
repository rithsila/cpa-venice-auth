package envelope

import (
	"encoding/json"
	"testing"
)

func TestOKWrapsResult(t *testing.T) {
	raw, err := OK(map[string]string{"identifier": "venice"})
	if err != nil {
		t.Fatalf("OK: %v", err)
	}
	env, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !env.OK {
		t.Fatal("ok = false, want true")
	}
	if env.Error != nil {
		t.Fatalf("error = %v, want nil", env.Error)
	}
	var got map[string]string
	if err := json.Unmarshal(env.Result, &got); err != nil {
		t.Fatalf("result: %v", err)
	}
	if got["identifier"] != "venice" {
		t.Fatalf("identifier = %q, want venice", got["identifier"])
	}
}

func TestFailCarriesCode(t *testing.T) {
	env, err := Decode(Fail("unknown_method", "nope"))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if env.OK {
		t.Fatal("ok = true, want false")
	}
	if env.Error == nil {
		t.Fatal("error = nil")
	}
	if env.Error.Code != "unknown_method" {
		t.Fatalf("code = %q, want unknown_method", env.Error.Code)
	}
	if env.Error.Message != "nope" {
		t.Fatalf("message = %q, want nope", env.Error.Message)
	}
}

func FuzzEnvelopeDecode(f *testing.F) {
	// Seed corpus with valid and invalid payloads
	f.Add([]byte(`{"ok":true,"result":{"foo":"bar"}}`))
	f.Add([]byte(`{"ok":false,"error":{"code":"test","message":"fail"}}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{"ok":true}`))
	f.Add([]byte(`{"ok":false}`))
	f.Add([]byte(`garbage not json`))
	f.Add([]byte(`{"ok": true, "result": 12345}`))
	f.Add([]byte(`{"ok": false, "error": null}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		// Decode should never panic, regardless of input
		env, err := Decode(data)
		if err == nil && env != nil {
			_ = env.OK
			if env.Error != nil {
				_ = env.Error.Code
				_ = env.Error.Message
			}
		}
	})
}
