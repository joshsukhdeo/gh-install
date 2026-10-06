# Implementation Plan: Universal Asset Classifier & Visual Categorization

## Overview
Implement a single universal classifier class (`AssetClassifier`) in `selector/classifier.go` that encapsulates all platform, OS, architecture, sidecar, checksum, and installation category detection logic. Integrate this single engine into `gh-pt show --assets`, replacing fragmented regexes and categorization logic across the codebase.

## Major Components & Dependencies
1. **Universal Classifier Class (`selector/classifier.go`)**:
   - `AssetClassifier` struct with host awareness (`HostOS`, `HostArch`, `WineMode`, `AvxLevel`).
   - OS Detection: Linux (and distros), Windows, macOS/Darwin, FreeBSD, Android, iOS, Raspberry Pi, Multiplatform, Unknown.
   - OS Display Formatting: Emoji mode (`🐧`, `🪟`, `🍎`, `😈`, `🤖`, `📱`, `🍓`, `🔀`, `❓`) vs Plain ID mode (`[linux]`, `[windows]`, etc.).
   - Subcategory Detection: `[NAT]`, `[EMU]`, `[WINE]` (suppressed on Windows), `[FRG]`, `[SIDE]`, `[SUM]`, `[SIG]`, `[META]`, `[UNS]`, `[UNK]`.
   - Top-Level Category Detection: Default Install vs Sidecar Install (-s) vs Installable Alternative vs Cannot/Should Not Install.
   - Unit tests covering `gz83/thorium` and `openvinotoolkit/model_server`.

2. **Integration with Core Selector & Existing APIs (`selector/category.go`)**:
   - Route `CategorizeAsset`, `AssetCategoryPriority`, `FormatCategorizedItem` through `DefaultClassifier()`.
   - Ensure backward compatibility with existing selector tests while unifying detection.

3. **Visual Output Renderer in `gh-pt show` (`cmd/show.go`)**:
   - In `showInfoWithClient`, when `showAssets` is triggered:
     - Group assets by the 4 top-level categories.
     - Within each category, group and sort by the 10 subcategories in the required order.
     - Highlight subcategories with distinct colors using `pterm`.
     - Omit any category or subcategory with 0 assets.
     - Respect `--no-emojis`, `--no-icons`, and `--no-color`.

4. **Integration Tests (`cmd/show_test.go` & `selector/classifier_test.go`)**:
   - Full test matrix on `gz83/thorium` (49 assets) and `openvinotoolkit/model_server` (16 assets).
   - Display formatting tests (emojis vs plain, Windows vs non-Windows).

## Implementation Order
1. Create `selector/classifier.go` with `AssetClassifier` struct and all detection methods.
2. Create `selector/classifier_test.go` with tests for OS detection, subcategories, installation categories, and the two target repos.
3. Refactor `selector/category.go` to delegate to `AssetClassifier`.
4. Update `cmd/show.go` to use `AssetClassifier` for the visual breakdown.
5. Add CLI integration tests in `cmd/show_test.go`.
6. Run `make fmt`, `make lint`, and `go test ./...` to verify everything builds and passes.

## Verification Checkpoints
- **Checkpoint 1**: `go test -v ./selector/...` passes all classification, OS, and repo fixture tests.
- **Checkpoint 2**: `go test -v ./cmd/...` passes all `show` display and flag formatting tests.
- **Checkpoint 3**: Full test suite `go test ./...` and `make lint` clean.
