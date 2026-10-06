# Task Breakdown: Universal Asset Classifier & Visual Categorization

- [x] Task 1: Implement Universal `AssetClassifier` Class in `selector/classifier.go`
  - Acceptance: `AssetClassifier` struct encapsulates OS detection (including Android, iOS, Raspberry Pi, Multiplat, Unknown), 10 subcategories (`[NAT]`, `[EMU]`, `[WINE]`, `[FRG]`, `[SIDE]`, `[SUM]`, `[SIG]`, `[META]`, `[UNS]`, `[UNK]`), 4 install categories, and OS icon formatting (emoji vs plain `[os-id]`).
  - Verify: Compiles cleanly with standard library and existing packages.
  - Files: `selector/classifier.go`

- [x] Task 2: Add TDD Unit Tests for `AssetClassifier` in `selector/classifier_test.go`
  - Acceptance: Unit tests covering OS table tests, subcategory mappings, Windows vs non-Windows behavior, and full test fixtures for `gz83/thorium` (49 assets) and `openvinotoolkit/model_server` (16 assets).
  - Verify: `rtk go test -v -run TestAssetClassifier ./selector/...` passes 100%.
  - Files: `selector/classifier_test.go`

- [x] Task 3: Unify Existing Category Engine via `AssetClassifier`
  - Acceptance: `selector/category.go` delegates to `AssetClassifier.Default()` ensuring single universal source of truth across `install`, `update`, and `selector`.
  - Verify: `rtk go test -v ./selector/...` passes all tests without regressions.
  - Files: `selector/category.go`

- [x] Task 4: Integrate Visual 4-Category Display into `gh-pt show --assets`
  - Acceptance: `showInfoWithClient` renders the 4 categories, groups/sorts by the subcategories, colors subcategories with `pterm`, omits empty sections, and respects `--no-emojis` / `--no-icons` / `--no-color`.
  - Verify: `rtk go test -v -run TestShowInfo_ShowAssets ./cmd/...` passes.
  - Files: `cmd/show.go`

- [x] Task 5: Add CLI Integration Tests for Categorized Assets Display
  - Acceptance: `cmd/show_test.go` verifies 4 visual categories, subcategory ordering, emoji vs plain OS tags, omitted empty sections, and Windows suppression of Wine.
  - Verify: `rtk go test -v ./cmd/...` passes.
  - Files: `cmd/show_test.go`

- [x] Task 6: Full Suite Verification & Build
  - Acceptance: Zero lint errors, 100% tests passing, clean binary compilation.
  - Verify: `make fmt && rtk make lint && rtk go test ./... && make build`.
  - Files: N/A
