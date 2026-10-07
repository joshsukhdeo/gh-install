# release/ — Release Resolution

## Files
- `release.go` — GitHub release fetching and parsing

## Responsibilities
- Fetch latest release tags via GraphQL
- Resolve release metadata (version, assets, body)
- Handle rate limiting and pagination
- Verify checksums if enabled

## Key Functions
- `FetchLatestReleaseTags(client, repos)` → batch GraphQL query
- `GetRelease(owner, repo, tag)` → full release data
- `VerifyChecksum(asset, expected)` → SHA256 validation