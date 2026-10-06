# SPEC-006: Cryptographic Verification State Machine & Unsigned Policy Enforcement

## 1. Objective
Isolate the cryptographic verification state machine into a dedicated `verification.Verifier` class, supporting all verification methods encountered in GitHub releases (SHA-256/SHA-512 checksums, PEM/PEP signatures, Sigstore Rekor keyless bundles, and GitHub commit lineage comparison for unsigned downloads). Restrict the `--insecure-allow-unsigned` CLI override to a strict per-item policy evaluation rather than globally overriding previously signed installations.

## 2. Requirements & Verification Matrix

### 2.1 Cryptographic Hierarchy
1. **Checksum Verification**:
   - SHA-256 (64 hex characters) and SHA-512 (128 hex characters) validation against assets.
2. **Signatureless Keystore Verification**:
   - Sigstore OIDC & Rekor transparency log bundle verification (`*.sigstore`, `*.sigstore.json`).
3. **Static PEM / PEP Signature Verification**:
   - Public key unmarshaling (`cryptoutils.UnmarshalPEMToPublicKey`) and signature verification via `signature.LoadVerifier(pubKey, crypto.SHA256)`.
4. **Commit Lineage Verification (Insecure Signatureless Downloads)**:
   - When updating without signatures, invoke GitHub Commits Compare API (`/repos/:owner/:repo/compare/:base...:head`).
   - If status is `diverged`, execution is strictly halted (potential repository hijack / history rewrite).
   - If status is `ahead` or `identical`, lineage authenticity is preserved.

### 2.2 Per-Item Policy Enforcement
- **Flag Renaming**: The flag `--skip-checksums` is mapped to `--insecure-allow-unsigned`, keeping `--skip-checksums` as an alias.
- **Bulk Upgrades**: Running `gh-pt upgrade --insecure-allow-unsigned` permits unsigned downloads only for packages that genuinely lacked signatures in `state.json` (`WasSigned == false`). Packages that previously possessed cryptographic verification (`WasSigned == true`) strictly enforce cryptographic proofs.
- **Targeted Overrides**: Running `gh-pt upgrade owner/repo --insecure-allow-unsigned` or `gh-pt install owner/repo --insecure-allow-unsigned` explicitly targets the package, applying the user's manual override.

## 3. Architecture & Contracts

### 3.1 `verification.Verifier`
```go
type Verifier struct {
    client                HTTPClient
    InsecureAllowUnsigned bool
    Logger                Logger
}

func NewVerifier(args ...any) *Verifier
func (v *Verifier) VerifyArtifact(data []byte, expectedChecksum string, sigBytes []byte, pubKeyBytes []byte) error
func (v *Verifier) Verify(a ArtifactContext) error
```

### 3.2 `state.InstalledApp` Policy Hook
```go
func (app *InstalledApp) AllowsUnsigned(globalAllowUnsigned bool, targeted bool) bool {
    if targeted {
        return globalAllowUnsigned || app.InsecureAllowUnsigned
    }
    if app.WasSigned {
        return false
    }
    return globalAllowUnsigned || app.InsecureAllowUnsigned
}
```
