package host

import (
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	wallet := "0x" + strings.Repeat("a", 64)
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "bearer token",
			in:   "Authorization: Bearer abc.def-123",
			want: "Authorization: Bearer REDACTED",
		},
		{
			name: "api key prefix",
			in:   "using sk-1234567890abcdef for auth",
			want: "using REDACTED for auth",
		},
		{
			name: "venice key prefix",
			in:   "using vnc_1234567890abcdef for auth",
			want: "using REDACTED for auth",
		},
		{
			name: "wallet private key",
			in:   "wallet " + wallet,
			want: "wallet REDACTED",
		},
		{
			name: "x402 header",
			in:   "SIGN-IN-WITH-X: eyJhbGciOiJIUzI1NiJ9",
			want: "SIGN-IN-WITH-X: REDACTED",
		},
		{
			name: "nothing to redact",
			in:   "plugin registered",
			want: "plugin registered",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Redact(tc.in); got != tc.want {
				t.Fatalf("Redact(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestLogFallsBackWithoutHost(t *testing.T) {
	// A nil callback must not panic and must not drop the message.
	api := New(nil)
	api.Log("info", "hello %s", "world")
}

func TestCallWithoutHost(t *testing.T) {
	api := New(nil)
	if _, err := api.Call("host.log", nil); err == nil {
		t.Fatal("Call returned nil error, want unavailable")
	}
}
