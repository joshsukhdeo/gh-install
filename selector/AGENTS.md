# selector/ — Asset Selection Logic

## Files
- `selector.go` — Asset matching and selection

## Responsibilities
- Match release assets to platform/arch
- Handle extractor selection (tar, zip, gz, etc.)
- Apply fallback releases if primary fails
- Warn on unmapped assets

## Key Functions
- `SelectAsset(release, platform, arch)` → best matching asset
- `GetExtractor(asset)` → extraction command
- `FallbackReleases()` → alternative release sources