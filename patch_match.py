import re

with open('selector/interface.go', 'r') as f:
    content = f.read()

structs = """
type AssetMatchCriteria struct {
	ReleaseId        int
	Name             string
	Regexps          []string
	Interactive      bool
	AllowForeignArch bool
}

type BinaryMatchCriteria struct {
	DownloadPath  string
	Names         []string
	Matcher       string
	Interactive   bool
	NativeExtract bool
}

func AssetSelector(ghClient GithubClient, repo string, criteria AssetMatchCriteria) (ISelector, error) {
"""
content = re.sub(r'func AssetSelector\(ghClient GithubClient, repo string,\s*releaseId int, name string, matchers \[\]string, interactive bool, allowForeignArch bool\) \(ISelector, error\) {', structs, content)
content = content.replace('releaseId', 'criteria.ReleaseId')
content = content.replace('name', 'criteria.Name')
content = content.replace('matchers', 'criteria.Regexps')
content = content.replace('interactive', 'criteria.Interactive')
content = content.replace('allowForeignArch', 'criteria.AllowForeignArch')

binary_func = """func BinarySelector(criteria BinaryMatchCriteria) (ISelector, error) {"""
content = re.sub(r'func BinarySelector\(downloadPath string, names \[\]string, matcher string, interactive bool, nativeExtract bool\) \(ISelector, error\) {', binary_func, content)
content = content.replace('downloadPath', 'criteria.DownloadPath')
content = content.replace('names', 'criteria.Names')
content = content.replace('matcher', 'criteria.Matcher')
content = content.replace('nativeExtract', 'criteria.NativeExtract')

with open('selector/interface.go', 'w') as f:
    f.write(content)

with open('release/release.go', 'r') as f:
    rel = f.read()

rel_asset_call = """	assetSelector, err := selector.AssetSelector(r.Client, r.CliParams.Repository, selector.AssetMatchCriteria{
		ReleaseId:        releases[0].Id,
		Name:             r.CliParams.ReleaseAsset,
		Regexps:          r.CliParams.ReleaseAssetRegexps,
		Interactive:      r.CliParams.Interactive,
		AllowForeignArch: r.CliParams.AllowForeignArch,
	})"""
rel = re.sub(r'\s*assetSelector, err := selector\.AssetSelector\(r\.Client, r\.CliParams\.Repository, releases\[0\]\.Id,\s*r\.CliParams\.ReleaseAsset, r\.CliParams\.ReleaseAssetRegexps, r\.CliParams\.Interactive, r\.CliParams\.AllowForeignArch\)', '\n' + rel_asset_call, rel)

rel_bin_call = """		binarySelector, execErr := selector.BinarySelector(selector.BinaryMatchCriteria{
			DownloadPath:  filepath.Join(downloadDir, asset.Name),
			Names:         r.CliParams.AssetBinaries,
			Matcher:       r.CliParams.AssetBinariesRegexp,
			Interactive:   r.CliParams.Interactive,
			NativeExtract: r.CliParams.NativeExtract,
		})"""
rel = re.sub(r'\s*binarySelector, execErr := selector\.BinarySelector\(filepath\.Join\(downloadDir,\s*asset\.Name\),\s*r\.CliParams\.AssetBinaries,\s*r\.CliParams\.AssetBinariesRegexp,\s*r\.CliParams\.Interactive,\s*r\.CliParams\.NativeExtract,\s*\)', '\n' + rel_bin_call, rel)

with open('release/release.go', 'w') as f:
    f.write(rel)
