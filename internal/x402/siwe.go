package x402

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"golang.org/x/crypto/sha3"
)

const (
	// DefaultDomain is the EIP-4361 domain for Venice AI.
	DefaultDomain = "api.venice.ai"
	// DefaultChainID is the Base mainnet EVM chain ID (8453).
	DefaultChainID = 8453
	// MaxClockDrift is the maximum allowable drift between local and server clock.
	MaxClockDrift = 60 * time.Second
)

// Signer signs EIP-4361 SIWE requests for Venice x402 wallet authentication.
type Signer struct {
	privKey       *secp256k1.PrivateKey
	address       string
	domain        string
	chainID       int
	uri           string
	mu            sync.RWMutex
	clockOffset   time.Duration
	hasServerTime bool
}

// NewSigner creates an EIP-4361 signer from a hex-encoded secp256k1 private key.
func NewSigner(privKeyHex string) (*Signer, error) {
	privKeyHex = strings.TrimPrefix(strings.TrimSpace(privKeyHex), "0x")
	rawKey, err := hex.DecodeString(privKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid hex private key: %w", err)
	}
	if len(rawKey) != 32 {
		return nil, fmt.Errorf("invalid private key length: expected 32 bytes, got %d", len(rawKey))
	}

	privKey := secp256k1.PrivKeyFromBytes(rawKey)
	address := PubKeyToAddress(privKey.PubKey())

	return &Signer{
		privKey: privKey,
		address: address,
		domain:  DefaultDomain,
		chainID: DefaultChainID,
	}, nil
}

// Address returns the checksummed Ethereum address of the signer.
func (s *Signer) Address() string {
	return s.address
}

// SIWXPayload represents the JSON object encoded in the SIGN-IN-WITH-X header.
type SIWXPayload struct {
	Address   string `json:"address"`
	Message   string `json:"message"`
	Signature string `json:"signature"`
	Timestamp int64  `json:"timestamp"`
	ChainID   int    `json:"chainId"`
}

// BuildMessage constructs the exact EIP-4361 text as Venice expects.
func (s *Signer) BuildMessage(issuedAt, expiresAt time.Time, nonce string) string {
	stamp := func(t time.Time) string {
		return t.UTC().Format("2006-01-02T15:04:05.000Z")
	}

	return fmt.Sprintf(
		"%s wants you to sign in with your Ethereum account:\n"+
			"%s\n\n"+
			"Sign in to Venice AI\n\n"+
			"URI: https://%s\n"+
			"Version: 1\n"+
			"Chain ID: %d\n"+
			"Nonce: %s\n"+
			"Issued At: %s\n"+
			"Expiration Time: %s",
		s.domain,
		s.address,
		s.domain,
		s.chainID,
		nonce,
		stamp(issuedAt),
		stamp(expiresAt),
	)
}

// UpdateClockSkew records the Venice server's Date header and computes clock drift.
func (s *Signer) UpdateClockSkew(dateHeader string) error {
	dateHeader = strings.TrimSpace(dateHeader)
	if dateHeader == "" {
		return nil
	}

	t, err := http.ParseTime(dateHeader)
	if err != nil {
		return fmt.Errorf("parse server date header %q: %w", dateHeader, err)
	}

	offset := time.Now().UTC().Sub(t.UTC())

	s.mu.Lock()
	s.clockOffset = offset
	s.hasServerTime = true
	s.mu.Unlock()

	return nil
}

// ClockSkew returns the currently measured clock offset from server time.
func (s *Signer) ClockSkew() (time.Duration, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.clockOffset, s.hasServerTime
}

// GenerateHeader creates a fresh, base64-encoded SIGN-IN-WITH-X header value.
// It generates a new cryptographically random nonce and uses current UTC time.
// It enforces that local clock has not drifted more than MaxClockDrift from server time.
func (s *Signer) GenerateHeader() (string, error) {
	s.mu.RLock()
	hasServer := s.hasServerTime
	offset := s.clockOffset
	s.mu.RUnlock()

	if hasServer && (offset > MaxClockDrift || offset < -MaxClockDrift) {
		return "", fmt.Errorf("clock skew detected: local clock drifts by %v (> %v) from Venice server time", offset, MaxClockDrift)
	}

	now := time.Now().UTC()
	expiresAt := now.Add(4 * time.Minute)

	nonceBytes := make([]byte, 8)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	nonce := hex.EncodeToString(nonceBytes)

	message := s.BuildMessage(now, expiresAt, nonce)
	sig, err := s.SignMessage(message)
	if err != nil {
		return "", fmt.Errorf("sign message: %w", err)
	}

	payload := SIWXPayload{
		Address:   s.address,
		Message:   message,
		Signature: sig,
		Timestamp: now.UnixMilli(),
		ChainID:   s.chainID,
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode siwx payload: %w", err)
	}

	return base64.StdEncoding.EncodeToString(raw), nil
}

// SignMessage signs a text message using the personal_sign (EIP-191) prefix.
func (s *Signer) SignMessage(message string) (string, error) {
	msgBytes := []byte(message)
	prefix := fmt.Sprintf("\x19Ethereum Signed Message:\n%d", len(msgBytes))
	hash := Keccak256(append([]byte(prefix), msgBytes...))

	sig := ecdsa.SignCompact(s.privKey, hash, false)

	// SignCompact returns [header (1 byte)][R (32 bytes)][S (32 bytes)]
	// where header = 27 + v + (4 if compressed).
	// In Ethereum personal_sign, standard wire format is [R (32)][S (32)][V (1)]
	// where V is 27 or 28 (or 0/1 depending on client, standard personal_sign hex is 27/28).
	v := sig[0] - 27
	if v >= 4 {
		v -= 4
	}
	ethV := v + 27

	ethSig := make([]byte, 65)
	copy(ethSig[0:32], sig[1:33])
	copy(ethSig[32:64], sig[33:65])
	ethSig[64] = ethV

	return "0x" + hex.EncodeToString(ethSig), nil
}

// RecoverAddress recovers the Ethereum address from an EIP-191 personal_sign signature.
func RecoverAddress(message string, sigHex string) (string, error) {
	sigHex = strings.TrimPrefix(strings.TrimSpace(sigHex), "0x")
	rawSig, err := hex.DecodeString(sigHex)
	if err != nil || len(rawSig) != 65 {
		return "", fmt.Errorf("invalid signature bytes: %v", err)
	}

	r := rawSig[0:32]
	s := rawSig[32:64]
	v := rawSig[64]
	if v >= 27 {
		v -= 27
	}

	// Reconstruct compact signature for decred/secp256k1:
	// header = 27 + v
	compactSig := make([]byte, 65)
	compactSig[0] = 27 + v
	copy(compactSig[1:33], r)
	copy(compactSig[33:65], s)

	msgBytes := []byte(message)
	prefix := fmt.Sprintf("\x19Ethereum Signed Message:\n%d", len(msgBytes))
	hash := Keccak256(append([]byte(prefix), msgBytes...))

	pubKey, _, err := ecdsa.RecoverCompact(compactSig, hash)
	if err != nil {
		return "", fmt.Errorf("recover compact: %w", err)
	}

	return PubKeyToAddress(pubKey), nil
}

// PubKeyToAddress derives the lowercase 0x-prefixed hex address from a secp256k1 public key.
func PubKeyToAddress(pubKey *secp256k1.PublicKey) string {
	rawPub := pubKey.SerializeUncompressed()
	// Strip 0x04 uncompressed prefix
	hash := Keccak256(rawPub[1:])
	// Take last 20 bytes
	addrBytes := hash[12:32]
	return ToChecksumAddress(addrBytes)
}

// Keccak256 computes the 32-byte Keccak-256 hash.
func Keccak256(data ...[]byte) []byte {
	h := sha3.NewLegacyKeccak256()
	for _, b := range data {
		h.Write(b)
	}
	return h.Sum(nil)
}

// ToChecksumAddress converts a 20-byte address to EIP-55 checksum format.
func ToChecksumAddress(addrBytes []byte) string {
	unchecksummed := hex.EncodeToString(addrBytes)
	hash := Keccak256([]byte(unchecksummed))

	var b strings.Builder
	b.WriteString("0x")
	for i := 0; i < len(unchecksummed); i++ {
		c := unchecksummed[i]
		if c >= '0' && c <= '9' {
			b.WriteByte(c)
		} else {
			hashNibble := (hash[i/2] >> (4 * (1 - (i % 2)))) & 0x0f
			if hashNibble >= 8 {
				b.WriteByte(c - 32) // uppercase
			} else {
				b.WriteByte(c)
			}
		}
	}
	return b.String()
}
