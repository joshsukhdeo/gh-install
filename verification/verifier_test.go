package verification

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"io"
	"net/http"
	"strings"
	"testing"
)

// MockHTTPClient intercepts GitHub API calls for Lineage Verification
type MockHTTPClient struct {
	ResponseStatus string // "ahead", "identical", "behind", or "diverged"
}

func (m *MockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	jsonBody := `{"status": "` + m.ResponseStatus + `"}`
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(jsonBody)),
	}, nil
}

type testLogger struct {
	messages []string
}

func (tl *testLogger) Println(v ...any) {
	var parts []string
	for _, item := range v {
		parts = append(parts, item.(string))
	}
	tl.messages = append(tl.messages, strings.Join(parts, " "))
}

func (tl *testLogger) Warn(msg any, keyvals ...any) {
	tl.messages = append(tl.messages, msg.(string))
}

func TestVerifyArtifact_ValidChecksum(t *testing.T) {
	v := NewVerifier(false)
	data := []byte("deterministic test payload")
	hash := sha256.Sum256(data)
	checksum := hex.EncodeToString(hash[:])

	err := v.VerifyArtifact(data, checksum, nil, nil)
	if err != nil {
		t.Fatalf("expected nil, got error: %v", err)
	}

	hash512 := sha512.Sum512(data)
	checksum512 := hex.EncodeToString(hash512[:])
	err = v.VerifyArtifact(data, checksum512, nil, nil)
	if err != nil {
		t.Fatalf("expected nil for valid SHA512, got error: %v", err)
	}
}

func TestVerifyArtifact_InvalidChecksum(t *testing.T) {
	v := NewVerifier(false)
	data := []byte("deterministic test payload")

	err := v.VerifyArtifact(data, "badhash", nil, nil)
	if err == nil {
		t.Fatal("expected error for invalid checksum, got nil")
	}
}

func TestVerifyArtifact_Unsigned_Block(t *testing.T) {
	v := NewVerifier(false)

	err := v.VerifyArtifact([]byte("data"), "", nil, nil)
	if err == nil || err.Error() != "artifact is unsigned and insecure-allow-unsigned is false" {
		t.Fatalf("expected unsigned block error, got: %v", err)
	}
}

func TestVerifyArtifact_Unsigned_AllowInsecure(t *testing.T) {
	v := NewVerifier(true)

	err := v.VerifyArtifact([]byte("data"), "", nil, nil)
	if err != nil {
		t.Fatalf("expected nil when allowing insecure unsigned, got: %v", err)
	}
}

func TestVerifier_SHA256Checksum(t *testing.T) {
	v := NewVerifier(&MockHTTPClient{})
	data := []byte("deterministic payload")
	hash := sha256.Sum256(data)

	artifact := ArtifactContext{
		Data:     data,
		Checksum: hex.EncodeToString(hash[:]),
	}

	if err := v.Verify(artifact); err != nil {
		t.Fatalf("expected valid checksum to pass, got: %v", err)
	}
}

func TestVerifier_Unsigned_PerItemEnforcement(t *testing.T) {
	v := NewVerifier(&MockHTTPClient{})
	data := []byte("unsigned payload")

	// 1. Global flag is true, but item specifically disallows it
	artifactStrict := ArtifactContext{
		Data:                data,
		GlobalAllowUnsigned: true,
		ItemAllowsUnsigned:  false, // e.g., state.json knows this usually has a signature
	}
	if err := v.Verify(artifactStrict); err == nil {
		t.Fatal("expected strict item to block unsigned artifact regardless of global flag")
	}

	// 2. Global flag is false, but item allows it (explicit override)
	artifactAllowed := ArtifactContext{
		Data:                data,
		GlobalAllowUnsigned: false,
		ItemAllowsUnsigned:  true,
	}
	if err := v.Verify(artifactAllowed); err != nil {
		t.Fatalf("expected allowed item to pass, got: %v", err)
	}
}

func TestVerifier_Lineage_DivergedBlock(t *testing.T) {
	v := NewVerifier(&MockHTTPClient{ResponseStatus: "diverged"})
	data := []byte("unsigned payload")
	hash := sha256.Sum256(data)

	artifact := ArtifactContext{
		Data:                data,
		Checksum:            hex.EncodeToString(hash[:]),
		GlobalAllowUnsigned: true,
		ItemAllowsUnsigned:  true,
		Repo:                "owner/repo",
		OldCommit:           "abc1234",
		NewCommit:           "def5678",
	}

	err := v.Verify(artifact)
	if err == nil || !strings.Contains(err.Error(), "diverged") {
		t.Fatalf("expected verification to strictly block diverged lineage, got: %v", err)
	}
}

func TestVerifier_Lineage_AheadPass(t *testing.T) {
	v := NewVerifier(&MockHTTPClient{ResponseStatus: "ahead"})
	data := []byte("unsigned payload")
	hash := sha256.Sum256(data)

	artifact := ArtifactContext{
		Data:                data,
		Checksum:            hex.EncodeToString(hash[:]),
		GlobalAllowUnsigned: true,
		ItemAllowsUnsigned:  true,
		Repo:                "owner/repo",
		OldCommit:           "abc1234",
		NewCommit:           "def5678",
	}

	if err := v.Verify(artifact); err != nil {
		t.Fatalf("expected 'ahead' lineage to pass verification, got: %v", err)
	}
}

func TestVerifier_CustomLogger_Warning(t *testing.T) {
	tl := &testLogger{}
	v := NewVerifier(true)
	v.Logger = tl

	err := v.VerifyArtifact([]byte("payload"), "", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tl.messages) == 0 {
		t.Fatal("expected custom logger to capture warning message")
	}
	if !strings.Contains(tl.messages[0], "--insecure-allow-unsigned") {
		t.Fatalf("expected warning about insecure-allow-unsigned, got: %s", tl.messages[0])
	}
}

func TestVerifier_PEMSignature(t *testing.T) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	pubKeyBytes, err := x509.MarshalPKIXPublicKey(&privKey.PublicKey)
	if err != nil {
		t.Fatalf("failed to marshal pub key: %v", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubKeyBytes,
	})

	data := []byte("payload to sign")
	hash := sha256.Sum256(data)
	sig, err := privKey.Sign(rand.Reader, hash[:], crypto.SHA256)
	if err != nil {
		t.Fatalf("failed to sign: %v", err)
	}

	v := NewVerifier(false)
	err = v.VerifyArtifact(data, "", sig, pubPEM)
	if err != nil {
		t.Fatalf("expected valid PEM signature to pass, got: %v", err)
	}

	err = v.VerifyArtifact(data, "", []byte("invalid-sig"), pubPEM)
	if err == nil {
		t.Fatal("expected invalid PEM signature to fail, got nil")
	}
}

func TestVerifier_ForceBypass_FallbackLineage_AheadPass(t *testing.T) {
	v := NewVerifier(&MockHTTPClient{ResponseStatus: "ahead"})
	data := []byte("payload with broken checksum")

	artifact := ArtifactContext{
		Data:                data,
		Checksum:            "badhash-that-would-normally-fail",
		GlobalAllowUnsigned: true,
		ItemAllowsUnsigned:  true,
		ForceBypass:         true,
		Repo:                "owner/repo",
		OldCommit:           "abc1234",
		NewCommit:           "def5678",
	}

	err := v.Verify(artifact)
	if err != nil {
		t.Fatalf("expected force bypass with ahead lineage to pass, got: %v", err)
	}
}

func TestVerifier_ForceBypass_FallbackLineage_DivergedBlock(t *testing.T) {
	v := NewVerifier(&MockHTTPClient{ResponseStatus: "diverged"})
	data := []byte("payload with broken checksum")

	artifact := ArtifactContext{
		Data:                data,
		Checksum:            "badhash-that-would-normally-fail",
		GlobalAllowUnsigned: true,
		ItemAllowsUnsigned:  true,
		ForceBypass:         true,
		Repo:                "owner/repo",
		OldCommit:           "abc1234",
		NewCommit:           "def5678",
	}

	err := v.Verify(artifact)
	if err == nil || !strings.Contains(err.Error(), "diverged") {
		t.Fatalf("expected diverged lineage to be blocked even with force bypass, got: %v", err)
	}
}

func TestVerifier_ForceBypass_WithoutInsecure_Blocked(t *testing.T) {
	v := NewVerifier(&MockHTTPClient{ResponseStatus: "ahead"}, false)
	data := []byte("payload with broken checksum")

	// ForceBypass is true, but insecure flags are false
	artifact := ArtifactContext{
		Data:                data,
		Checksum:            "badhash",
		GlobalAllowUnsigned: false,
		ItemAllowsUnsigned:  false,
		ForceBypass:         true,
		Repo:                "owner/repo",
		OldCommit:           "abc1234",
		NewCommit:           "def5678",
	}

	err := v.Verify(artifact)
	if err == nil {
		t.Fatal("expected force bypass without insecure-allow-unsigned to fail standard verification")
	}
}
