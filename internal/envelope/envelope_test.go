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
