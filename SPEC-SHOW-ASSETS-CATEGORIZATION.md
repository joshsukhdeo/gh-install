# Spec: Visual Asset Categorization and Architecture Grouping for `gh-pt show --assets`

## Objective
Enhance `gh-pt show --assets` (and `gh-pt show <repo> --assets`) to provide a structured, visually distinct breakdown of release assets categorized into 4 top-level installation states, further segmented into granular compatibility and asset-type subcategories with operating system icons/IDs.

### 4 Top-Level Visual Categories
1. **Will be installed by default**: Primary binary or package release assets selected by `gh-pt`'s default installation engine (`release.MakeGithubRelease` / `selector.AssetSelector`) for the current host environment.
2. **Will be installed with `-s` (`--include-sidecars`)**: Companion sidecar assets (libraries, packs, plugins, models, configs) that would be auto-included when sidecar mode is active.
3. **Can be installed but will not be**: Runnable/installable alternative assets for the host or compatible environment (e.g., alternative instruction sets like AVX vs AVX2 vs SSE, alternate package formats like `.rpm` vs `.deb` vs `.AppImage`, or Wine executables) that are installable but not selected by default.
4. **Should not / cannot be installed**: Checksums (`.sha256`, `.md5`, `hashes.txt`), cryptographic signatures (`.sig`, `.asc`, `.pem`), source code archives (`source.zip`, `*-src.tar.gz`), metadata files, and incompatible OS targets (e.g., Android APKs or iOS IPAs on desktop).

*Empty Category Rule*: If a category has 0 assets, it is omitted from the display.

### Subcategories
Within each of the 4 categories, assets are grouped into subcategories and displayed in the following strict order (subcategories with 0 assets are omitted):
1. **`native [NAT]`**: Assets matching the host OS and host CPU architecture (`runtime.GOOS` + `runtime.GOARCH`).
2. **`emulated [EMU]`**: Assets matching architectures supported via host emulation:
   - On macOS (`darwin/arm64`): `x86_64` / `amd64` binaries (supported via Rosetta 2).
   - On Windows (`windows/arm64`): `x86` / `amd64` binaries (supported via Windows on ARM emulation).
   - On Linux: 32-bit x86 on `amd64`, or architectures flagged as emulated.
3. **`wine [WINE]`**: Windows binaries/archives runnable via Wine on non-Windows hosts (`linux`, `darwin`, `freebsd`). *Omitted entirely on Windows hosts.*
4. **`foreign [FRG]`**: Incompatible CPU architectures for the host OS (e.g., `arm64` on Linux `amd64`, `riscv64`, `s390x`, `ppc64le`).
5. **`sidecar [SIDE]`**: Companion dynamic libraries (`.so`, `.dylib`, `.dll`), resource packs (`.pak`), plugins, and data assets.
6. **`checksum/hash [SUM]`**: Hash files and digest verification documents (`.sha256`, `.sha512`, `.sha1`, `.md5`, `checksums.txt`, `hashes.txt`).
7. **`signature [SIG]`**: Cryptographic signatures and certificates (`.sig`, `.asc`, `.pem`, `.minisig`).
8. **`metadata [META]`**: Non-executable documentation, licenses, manifests, release notes, and source code archives (`source.zip`, `*-src.tar.gz`).
9. **`incompatible/unsupported [UNS]`**: Executables or packages for an entirely different, unsupported operating system (e.g., Android `.apk` on desktop, iOS `.ipa`, macOS `.dmg` on Linux).
10. **`unknown [UNK]`**: Assets with unrecognized architecture, OS, or type.

Distinct terminal colors highlight each subcategory (e.g., Green for `[NAT]`, Cyan for `[EMU]`, Yellow/Magenta for `[WINE]`, Light Red for `[FRG]`, Blue for `[SIDE]`, Gray for `[SUM]`, Purple for `[SIG]`, Dark Gray for `[META]`, Red for `[UNS]`, Light Yellow for `[UNK]`).

### OS Icons & Plain Identifiers
Each asset line displays an OS indicator immediately preceding the asset name:
- **Emoji Mode** (default):
  - Linux: `🐧`
  - Windows: `🪟`
  - macOS: `🍎`
  - FreeBSD: `😈`
  - Android: `🤖`
  - iOS: `📱`
  - Raspberry Pi: `🍓`
  - Multiplatform: `🔀`
  - Unknown: `❓`
- **Plain Identifier Mode** (triggered by `--no-emojis`, `--no-icons` [aliases], or `--no-color` [which implies `--no-emojis`]):
  - `[linux] $assetName`
  - `[windows] $assetName`
  - `[darwin] $assetName`
  - `[freebsd] $assetName`
  - `[android] $assetName`
  - `[ios] $assetName`
  - `[raspberrypi] $assetName`
  - `[multiplat] $assetName`
  - `[unknown] $assetName`

### Single Source of Truth
All classification logic (OS detection, architecture mapping, sidecar identification, checksum/signature detection, and installer matching) must be centralized in `selector/` and `release/` and reused across `gh-pt install`, `gh-pt update`, and `gh-pt show --assets`. There must be zero duplicated regexes or heuristics.

## Tech Stack
- Go 1.27.1 / Go standard library
- `github.com/pterm/pterm` for styled terminal output and color rendering
- `github.com/charmbracelet/log` for logging
- `github.com/stretchr/testify` for assertions and testing

## Commands
```bash
Build: make build
Test:  go test -v ./...
Lint:  make lint
Fmt:   make fmt
```

## Project Structure
```
selector/
  category.go          → Centralized asset classification: OS detection, Subcategory ([NAT], [EMU], [WINE], [FRG], [SIDE], [SUM], [SIG], [META], [UNS], [UNK]), and InstallCategory
  category_test.go     → Unit tests for OS detection, architecture resolution, and test fixtures for gz83/thorium & openvinotoolkit/model_server
cmd/
  show.go              → Visual display rendering in showInfoWithClient using pterm styling and category grouping
  show_test.go         → CLI tests verifying 4 categories, subcategory tags, emoji & plain modes, and Windows vs non-Windows behavior
```

## Code Style & Architecture
```go
type AssetOS string

const (
	OSLinux       AssetOS = "linux"
	OSWindows     AssetOS = "windows"
	OSDarwin      AssetOS = "darwin"
	OSFreeBSD     AssetOS = "freebsd"
	OSAndroid     AssetOS = "android"
	OSIOS         AssetOS = "ios"
	OSRaspberryPi AssetOS = "raspberrypi"
	OSMultiplat   AssetOS = "multiplat"
	OSUnknown     AssetOS = "unknown"
)

type ArchSubcategory string

const (
	SubcatNative       ArchSubcategory = "NAT"
	SubcatEmulated     ArchSubcategory = "EMU"
	SubcatWine         ArchSubcategory = "WINE"
	SubcatForeign      ArchSubcategory = "FRG"
	SubcatSidecar      ArchSubcategory = "SIDE"
	SubcatChecksum     ArchSubcategory = "SUM"
	SubcatSignature    ArchSubcategory = "SIG"
	SubcatMetadata     ArchSubcategory = "META"
	SubcatUnsupported  ArchSubcategory = "UNS"
	SubcatUnknown      ArchSubcategory = "UNK"
)

type InstallCategory int

const (
	CategoryDefaultInstall InstallCategory = iota
	CategorySidecarInstall
	CategoryInstallableAlternative
	CategoryCannotInstall
)

type CategorizedAsset struct {
	Name            string
	Size            int64
	OS              AssetOS
	Subcat          ArchSubcategory
	InstallCategory InstallCategory
}
```

## Testing Strategy
1. **OS Detection Table Tests** (`selector/category_test.go`):
   - Explicit tests verifying detection of Linux, Windows, macOS, FreeBSD, Android (`.apk`), iOS (`.ipa`), Raspberry Pi (`rpi`, `raspberry`), Multiplatform (`.jar`, universal scripts), and Unknown.
2. **Subcategory Classification Tests** (`selector/category_test.go`):
   - Native vs Emulated vs Wine vs Foreign vs Sidecar vs Checksum vs Signature vs Metadata vs Unsupported vs Unknown across Linux, macOS, and Windows runtime hosts.
3. **Repository Fixture Verifications** (`selector/category_test.go`):
   - `gz83/thorium` (49 assets): Android APKs &rarr; `[android]` `[UNS]`, macOS DMGs &rarr; `[darwin]`, Windows mini-installers &rarr; `[windows]` (`[WINE]` on Linux), Linux AppImages/debs/rpms &rarr; `[linux]` with host-matched AVX tier in `[NAT]` and other tiers in Alternative `[NAT]`.
   - `openvinotoolkit/model_server` (16 assets): `.sha256` checksums &rarr; `[SUM]` in `CategoryCannotInstall`, Ubuntu/RedHat tarballs &rarr; `[linux]`, Windows zip &rarr; `[windows]`.
4. **Visual & CLI Formatting Tests** (`cmd/show_test.go`):
   - Test output structure for 4 categories and ordered subcategories.
   - Verify empty categories and subcategories are omitted.
   - Verify `--no-icons`, `--no-emojis`, and `--no-color` render `[os-id] $assetName`.
   - Verify Windows environment excludes `wine [WINE]`.

## Boundaries
- **Always do**:
  - Centralize OS, architecture, and sidecar detection in `selector/` so `install`, `update`, and `show` share the exact same logic.
  - Omit categories and subcategories that contain 0 assets.
  - Ensure `--no-color` strips ANSI color escape sequences and implies `--no-emojis`.
  - Run `make fmt`, `make lint`, and `go test ./...` before considering complete.
- **Ask first**:
  - Adding new CLI flags or altering existing flag contracts.
  - Adding new external dependencies.
- **Never do**:
  - Duplicate regexes or detection logic between `cmd/` and `release/` or `selector/`.
  - Display `wine [WINE]` on native Windows.

## Success Criteria
1. `gh-pt show --assets` visually partitions assets into the 4 defined categories, omitting any that are empty.
2. Within each category, assets are grouped into:
   `native [NAT]`, `emulated [EMU]`, `wine [WINE]`, `foreign [FRG]`, `sidecar [SIDE]`, `checksum/hash [SUM]`, `signature [SIG]`, `metadata [META]`, `incompatible/unsupported [UNS]`, `unknown [UNK]`, in strict order, with `wine` excluded on Windows.
3. Subcategories are visually highlighted with distinct colors.
4. OS detection displays emojis by default and `[os-id]` in `--no-emojis` / `--no-icons` / `--no-color` mode.
5. All assets from `gz83/thorium` and `openvinotoolkit/model_server` are accurately categorized with zero ambiguity.
6. All existing and new tests pass cleanly, and linter reports 0 issues.
