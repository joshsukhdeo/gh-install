package verification

import (
	"bytes"
	"crypto"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/auth"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
	"github.com/sigstore/sigstore/pkg/cryptoutils"
	"github.com/sigstore/sigstore/pkg/signature"
)

// HTTPClient interface allows injecting mocks for the GitHub API
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Logger interface allows custom logging of verification events and warnings.
type Logger interface {
	Println(v ...any)
	Warn(msg any, keyvals ...any)
}

// ArtifactContext encapsulates all release metadata and cryptographic materials.
type ArtifactContext struct {
	Data                []byte
	Checksum            string
	SignaturePEM        []byte
	SigstoreBundle      []byte
	Repo                string
	OldCommit           string
	NewCommit           string
	GlobalAllowUnsigned bool
	ItemAllowsUnsigned  bool
	ForceBypass         bool
}

// Verifier orchestrates cryptographic integrity and provenance checks across downloaded artifacts.
type Verifier struct {
	client                HTTPClient
	InsecureAllowUnsigned bool
	Logger                Logger
}

// NewVerifier initializes the verification state machine supporting options:
// - NewVerifier(insecure bool)
// - NewVerifier(client HTTPClient)
// - NewVerifier(client HTTPClient, insecure bool)
func NewVerifier(args ...any) *Verifier {
	v := &Verifier{
		client: &http.Client{Timeout: 10 * time.Second},
	}
	for _, arg := range args {
		switch val := arg.(type) {
		case bool:
			v.InsecureAllowUnsigned = val
		case HTTPClient:
			if val != nil {
				v.client = val
			}
		case Logger:
			v.Logger = val
		}
	}
	return v
}

// VerifyArtifact executes the verification pipeline against raw bytes and signatures.
func (v *Verifier) VerifyArtifact(data []byte, expectedChecksum string, sigBytes []byte, pubKeyBytes []byte) error {
	// 1. Fallback: Completely Unsigned State
	if expectedChecksum == "" && len(sigBytes) == 0 {
		if v.InsecureAllowUnsigned {
			v.warn("[WARNING] Artifact is completely unsigned. Bypassing verification due to --insecure-allow-unsigned.")
			return nil
		}
		return fmt.Errorf("artifact is unsigned and insecure-allow-unsigned is false")
	}

	// 2. Compute SHA256 / SHA512 Digest
	hashBytes := sha256.Sum256(data)
	actualHash := hex.EncodeToString(hashBytes[:])
	checkHash := actualHash
	if len(expectedChecksum) == 128 {
		h512 := sha512.Sum512(data)
		checkHash = hex.EncodeToString(h512[:])
	}

	// 3. Verify Checksum
	if expectedChecksum != "" && !strings.EqualFold(checkHash, expectedChecksum) {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, checkHash)
	}

	// 4. Verify Cryptographic Signature (Sigstore / PEM)
	if len(sigBytes) > 0 && len(pubKeyBytes) > 0 {
		pubKey, err := cryptoutils.UnmarshalPEMToPublicKey(pubKeyBytes)
		if err != nil {
			return fmt.Errorf("failed to parse public key: %w", err)
		}

		sigVerifier, err := signature.LoadVerifier(pubKey, crypto.SHA256)
		if err != nil {
			return fmt.Errorf("failed to initialize signature verifier: %w", err)
		}

		if err := sigVerifier.VerifySignature(bytes.NewReader(sigBytes), bytes.NewReader(data)); err != nil {
			if err2 := sigVerifier.VerifySignature(bytes.NewReader(sigBytes), bytes.NewReader(hashBytes[:])); err2 != nil {
				return fmt.Errorf("cryptographic signature invalid: %w", err)
			}
		}
	}

	return nil
}

// Verify routes the artifact context through the complete cryptographic proof hierarchy.
func (v *Verifier) Verify(a ArtifactContext) error {
	// If ForceBypass is active alongside InsecureAllowUnsigned:
	// Bypass standard verification track, falling back strictly to commit lineage verification.
	if a.ForceBypass && (a.ItemAllowsUnsigned || a.GlobalAllowUnsigned || v.InsecureAllowUnsigned) {
		v.warn("[WARNING] Standard verification bypassed via --insecure-allow-unsigned --force. Falling back to commit lineage verification.")
		if a.OldCommit != "" && a.NewCommit != "" {
			return v.verifyCommitLineage(a.Repo, a.OldCommit, a.NewCommit)
		}
		return nil
	}

	// 1. Compute Base SHA256
	hashBytes := sha256.Sum256(a.Data)
	actualHash := hex.EncodeToString(hashBytes[:])

	// 2. Validate Checksum (if provided)
	if a.Checksum != "" {
		checkHash := actualHash
		if len(a.Checksum) == 128 {
			h512 := sha512.Sum512(a.Data)
			checkHash = hex.EncodeToString(h512[:])
		}
		if !strings.EqualFold(checkHash, a.Checksum) {
			return fmt.Errorf("checksum mismatch: expected %s, got %s", a.Checksum, checkHash)
		}
	}

	// 3. If commits are provided, verify the GitHub Commit Lineage authenticity
	if a.OldCommit != "" && a.NewCommit != "" {
		if err := v.verifyCommitLineage(a.Repo, a.OldCommit, a.NewCommit); err != nil {
			return err
		}
	}

	hasCryptoSignature := len(a.SignaturePEM) > 0 || len(a.SigstoreBundle) > 0

	// 4. Handle Completely Unsigned Artifacts
	if !hasCryptoSignature && a.Checksum == "" {
		if !a.ItemAllowsUnsigned {
			return fmt.Errorf("artifact is unsigned; blocked by strict item state (requires signature)")
		}
		v.warn("[WARNING] Artifact is completely unsigned. Bypassing verification due to --insecure-allow-unsigned.")
		return nil
	}

	// 5. Validate Signatureless Keystore (Sigstore OIDC/Rekor Bundle)
	if len(a.SigstoreBundle) > 0 {
		if err := v.verifyKeylessBundle(hashBytes[:], a.SigstoreBundle); err != nil {
			return fmt.Errorf("keyless bundle verification failed: %w", err)
		}
		return nil
	}

	// 6. Validate Static PEM / PEP Signature
	if len(a.SignaturePEM) > 0 {
		pubKey, err := cryptoutils.UnmarshalPEMToPublicKey(a.SignaturePEM)
		if err != nil {
			return fmt.Errorf("failed to parse PEM public key: %w", err)
		}

		sigVerifier, err := signature.LoadVerifier(pubKey, crypto.SHA256)
		if err != nil {
			return fmt.Errorf("failed to initialize PEM verifier: %w", err)
		}

		if err := sigVerifier.VerifySignature(bytes.NewReader(a.SignaturePEM), bytes.NewReader(a.Data)); err != nil {
			if err2 := sigVerifier.VerifySignature(bytes.NewReader(a.SignaturePEM), bytes.NewReader(hashBytes[:])); err2 != nil {
				return fmt.Errorf("PEM cryptographic signature invalid: %w", err)
			}
		}
	}

	return nil
}

func (v *Verifier) warn(msg string) {
	if v.Logger != nil {
		v.Logger.Println(msg)
	} else {
		log.Println(msg)
	}
}

// verifyCommitLineage calls the GitHub API to ensure the update isn't a repository hijack.
func (v *Verifier) verifyCommitLineage(repo, oldHash, newHash string) error {
	if oldHash == newHash {
		return nil // Identical, no change
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/compare/%s...%s", repo, oldHash, newHash)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create lineage request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if token, _ := auth.TokenForHost("github.com"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := v.client.Do(req)
	if err != nil {
		return fmt.Errorf("network error during lineage verification: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub API returned status: %s", resp.Status)
	}

	var payload struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return fmt.Errorf("failed to parse GitHub response: %w", err)
	}

	// Strictly block diverged histories.
	if payload.Status == "diverged" {
		return fmt.Errorf("security block: commit history has diverged (potential malicious rewrite)")
	}

	return nil
}

// verifyKeylessBundle processes modern Rekor transparency log proofs.
func (v *Verifier) verifyKeylessBundle(artifactDigest []byte, bundleBytes []byte) error {
	var b bundle.Bundle
	if err := b.UnmarshalJSON(bundleBytes); err != nil {
		return err
	}

	trustRoot, err := root.FetchTrustedRoot()
	if err != nil {
		return fmt.Errorf("failed to fetch trusted root: %w", err)
	}

	verifier, err := verify.NewVerifier(trustRoot, verify.WithTransparencyLog(1))
	if err != nil {
		return fmt.Errorf("failed to create signed entity verifier: %w", err)
	}

	policy := verify.NewPolicy(verify.WithArtifactDigest("sha256", artifactDigest), verify.WithoutIdentitiesUnsafe())
	_, err = verifier.Verify(&b, policy)
	return err
}
