package x402

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	// Known test private key (do not use with real funds).
	testPrivKeyHex = "4c0883a69102937d6231471b5dbb6204db7e716b5c6e0f4504b0484389fa4009"
	// Expected checksummed address for this private key.
	expectedAddress = "0xF01a4711A00D21f860fd58A4357F21Ee102f6214"
)

func TestSignerAddressDerivation(t *testing.T) {
	signer, err := NewSigner(testPrivKeyHex)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	if strings.ToLower(signer.Address()) != strings.ToLower(expectedAddress) {
		t.Fatalf("address = %s, want %s", signer.Address(), expectedAddress)
	}
}

func TestBuildMessageGolden(t *testing.T) {
	signer, err := NewSigner(testPrivKeyHex)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}

	issuedAt, _ := time.Parse(time.RFC3339, "2026-10-28T12:00:00Z")
	expiresAt, _ := time.Parse(time.RFC3339, "2026-10-28T12:05:00Z")
	nonce := "a1b2c3d4e5f60718"

	got := signer.BuildMessage(issuedAt, expiresAt, nonce)

	golden, err := os.ReadFile("testdata/eip4361_message.golden")
	if err != nil {
		t.Fatalf("read golden file: %v", err)
	}

	if got != string(golden) {
		t.Fatalf("BuildMessage mismatch with golden file:\nGOT:\n%s\n\nWANT:\n%s", got, string(golden))
	}
}

func TestBuildMessageEIP4361Format(t *testing.T) {
	signer, err := NewSigner(testPrivKeyHex)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}

	issuedAt, _ := time.Parse(time.RFC3339, "2026-10-28T12:00:00Z")
	expiresAt, _ := time.Parse(time.RFC3339, "2026-10-28T12:05:00Z")
	nonce := "a1b2c3d4e5f60718"

	msg := signer.BuildMessage(issuedAt, expiresAt, nonce)

	expectedPrefix := "api.venice.ai wants you to sign in with your Ethereum account:\n" +
		signer.Address() + "\n\n" +
		"Sign in to Venice AI\n\n" +
		"URI: https://api.venice.ai\n" +
		"Version: 1\n" +
		"Chain ID: 8453\n" +
		"Nonce: a1b2c3d4e5f60718\n" +
		"Issued At: 2026-10-28T12:00:00.000Z\n" +
		"Expiration Time: 2026-10-28T12:05:00.000Z"

	if msg != expectedPrefix {
		t.Fatalf("message mismatch:\nGOT:\n%s\n\nWANT:\n%s", msg, expectedPrefix)
	}
}

func TestSignAndRecoverAddress(t *testing.T) {
	signer, err := NewSigner(testPrivKeyHex)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}

	message := "Hello Venice AI"
	sig, err := signer.SignMessage(message)
	if err != nil {
		t.Fatalf("SignMessage: %v", err)
	}

	recovered, err := RecoverAddress(message, sig)
	if err != nil {
		t.Fatalf("RecoverAddress: %v", err)
	}

	if strings.ToLower(recovered) != strings.ToLower(signer.Address()) {
		t.Fatalf("recovered %s, want %s", recovered, signer.Address())
	}
}

func TestClockSkewGuard(t *testing.T) {
	signer, err := NewSigner(testPrivKeyHex)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}

	// 1. Initially no server time recorded -> signing succeeds
	if _, err := signer.GenerateHeader(); err != nil {
		t.Fatalf("expected header generation to succeed with no skew recorded: %v", err)
	}

	// 2. Server time within 30s drift -> succeeds
	now := time.Now().UTC()
	if err := signer.UpdateClockSkew(now.Add(30 * time.Second).Format(http.TimeFormat)); err != nil {
		t.Fatalf("UpdateClockSkew: %v", err)
	}
	if _, err := signer.GenerateHeader(); err != nil {
		t.Fatalf("expected header generation to succeed within 30s skew: %v", err)
	}

	// 3. Server time drifting ahead by 65s -> refuses to sign
	if err := signer.UpdateClockSkew(now.Add(-65 * time.Second).Format(http.TimeFormat)); err != nil {
		t.Fatalf("UpdateClockSkew: %v", err)
	}
	if _, err := signer.GenerateHeader(); err == nil {
		t.Fatal("expected GenerateHeader to fail when local clock is 65s ahead of server time")
	} else if !strings.Contains(err.Error(), "clock skew") {
		t.Fatalf("expected clock skew error message, got: %v", err)
	}

	// 4. Server time drifting behind by 70s -> refuses to sign
	if err := signer.UpdateClockSkew(now.Add(70 * time.Second).Format(http.TimeFormat)); err != nil {
		t.Fatalf("UpdateClockSkew: %v", err)
	}
	if _, err := signer.GenerateHeader(); err == nil {
		t.Fatal("expected GenerateHeader to fail when local clock is 70s behind server time")
	}

	// 5. Reset to aligned server time -> succeeds again
	if err := signer.UpdateClockSkew(time.Now().UTC().Format(http.TimeFormat)); err != nil {
		t.Fatalf("UpdateClockSkew: %v", err)
	}
	if _, err := signer.GenerateHeader(); err != nil {
		t.Fatalf("expected header generation to succeed after clock skew resolved: %v", err)
	}
}

func TestGenerateHeaderProducesFreshNonReusablePayloads(t *testing.T) {
	signer, err := NewSigner(testPrivKeyHex)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}

	h1, err := signer.GenerateHeader()
	if err != nil {
		t.Fatalf("GenerateHeader 1: %v", err)
	}
	h2, err := signer.GenerateHeader()
	if err != nil {
		t.Fatalf("GenerateHeader 2: %v", err)
	}

	// Two consecutive calls MUST produce different headers (different nonce, timestamp, signature)
	if h1 == h2 {
		t.Fatal("two consecutive GenerateHeader calls returned identical headers; nonce must not be cached")
	}

	// Verify decoding and signature validity
	rawBytes, err := base64.StdEncoding.DecodeString(h1)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}

	var p SIWXPayload
	if err := json.Unmarshal(rawBytes, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if p.ChainID != DefaultChainID {
		t.Errorf("chainId = %d, want 8453", p.ChainID)
	}
	if strings.ToLower(p.Address) != strings.ToLower(signer.Address()) {
		t.Errorf("address = %s, want %s", p.Address, signer.Address())
	}

	recovered, err := RecoverAddress(p.Message, p.Signature)
	if err != nil {
		t.Fatalf("recover from payload: %v", err)
	}
	if strings.ToLower(recovered) != strings.ToLower(signer.Address()) {
		t.Fatalf("recovered %s, want %s", recovered, signer.Address())
	}
}
