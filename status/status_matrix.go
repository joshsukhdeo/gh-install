package status

import (
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

type InstallState struct {
	InState           bool
	AlreadyInstalled  bool
	PrevVersion       string
	NewVersion        string
	AppName           string
	Type              string
	Repo              string
	AssetName         string
	Force             bool
	AllowDowngrade    bool
	// New downgrade flags
	LeRetrogrouch     bool
	RetrogradeStopgap bool
	Barbarous         bool
	SelfInflictedDebt bool
	IsUpgradeCmd      bool

	ExtractedAssets []string
	ArchiveType     string
	ArchiveName     string
	Sidecars        []string
}

// CompareVersions returns 1 if new > prev, -1 if new < prev, 0 if new == prev
func CompareVersions(prev, new string) int {
	if prev == new {
		return 0
	}
	p := prev
	if !strings.HasPrefix(p, "v") {
		p = "v" + p
	}
	n := new
	if !strings.HasPrefix(n, "v") {
		n = "v" + n
	}

	if semver.IsValid(p) && semver.IsValid(n) {
		return semver.Compare(n, p)
	}

	// Fallback to string comparison if not semver
	if new > prev {
		return 1
	}
	return -1
}

func GenerateStatusMessage(s InstallState) (string, error) {
	comp := CompareVersions(s.PrevVersion, s.NewVersion)

	anyDowngradeFlag := s.AllowDowngrade || s.SelfInflictedDebt || s.LeRetrogrouch || s.RetrogradeStopgap || s.Barbarous

	var versionSegment string
	if s.PrevVersion != "" {
		versionSegment = fmt.Sprintf("%s -> %s", s.PrevVersion, s.NewVersion)
	} else {
		versionSegment = fmt.Sprintf("-> %s", s.NewVersion)
	}

	var typeStr string
	if s.ArchiveType != "" {
		typeStr = fmt.Sprintf("%s/%s", s.ArchiveType, s.Type)
	} else {
		typeStr = s.Type
	}

	assetsStr := s.AppName
	if len(s.ExtractedAssets) > 0 {
		assetsStr = strings.Join(s.ExtractedAssets, ", ")
	} else if s.AssetName != "" {
		assetsStr = s.AssetName
	}

	var archiveStr string
	if s.ArchiveName != "" {
		archiveStr = fmt.Sprintf(" | %s", s.ArchiveName)
	}

	var sidecarStr string
	if len(s.Sidecars) > 0 {
		sidecarStr = fmt.Sprintf(" [sidecars: %s]", strings.Join(s.Sidecars, ", "))
	}

	baseStr := fmt.Sprintf("%s | %s [%s] from %s%s%s", versionSegment, assetsStr, typeStr, s.Repo, archiveStr, sidecarStr)

	if !s.InState {
		return fmt.Sprintf("INSTALLED ( %s )", baseStr), nil
	}

	if comp == 0 { // ZEROGRADE
		if s.Force {
			if s.AlreadyInstalled {
				if s.InState {
					return fmt.Sprintf("REINSTALLED ~~> 🟰ZEROGRADE🟰 ( %s )", baseStr), nil
				} else {
					return fmt.Sprintf("ADOPTED + REINSTALLED ~~> 🟰ZEROGRADE🟰 ( %s )", baseStr), nil
				}
			} else {
				if s.InState {
					return "REINSTALLED", nil
				} else {
					return "REINSTALLED & ADOPTED", nil
				}
			}
		} else {
			if s.AlreadyInstalled {
				if s.InState {
					return "⚠️ABORTION ~ ZEROGRADE REINSTALL subverted⚠️ => To avoid these abortions going forward, pass the -f or --force param to allow over-writing", fmt.Errorf("warning: Zerograde reinstall subverted")
				} else {
					return "⚠️ABORTION ~ ADOPT + ZEROGRADE REINSTALL subverted⚠️ => To avoid these abortions going forward, pass the -f or --force param to allow over-writing", fmt.Errorf("warning: Adopt + Zerograde reinstall subverted")
				}
			} else {
				if s.InState {
					return "RESTORING MISSING FILES", nil
				} else {
					return "ADOPTING & RESTORING", nil
				}
			}
		}
	} else if comp > 0 { // UPGRADE
		if s.InState {
			return fmt.Sprintf("REINSTALLED ~~> ✨UPGRADED✨ ( %s )", baseStr), nil
		} else {
			return fmt.Sprintf("ADOPTED + REINSTALLED ~~> ✨UPGRADED✨ ( %s )", baseStr), nil
		}

	} else { // DOWNGRADE
		if !s.Force {
			if s.InState {
				return "⚠️ABORTION ~ DOWNGRADE subverted⚠️ => To avoid these abortions going forward, the following largely undesirable options have been provided by the creator's magnanimity:\n➵use -f or --force to allow overwrites/re-installs\n➵use ---self-inflicted-technical-debt to allow downgrades\n➵use ---LE-RETROGROUCH to exclusively downgrades and save the item's entry with unpinned and with a flag exclusively (In the resulting saved state, the item is unpined with a 'Le_RetroGrouch' flag) [note that if the item is pinned, --unpin is required]\n➵use ---retograde-stopgap to unpin, exclusively downgrades and pin the resultant version.\n➵use ---BARBAROUS to bypass virustotal security scanning, bypass certifying hashes, allow wine, allow foreign architecture, and allow downgrades", fmt.Errorf("error: Downgrade subverted")
			} else {
				return "⚠️ABORTION ~ ADOPT + DOWNGRADE subverted⚠️ => To avoid these abortions going forward, the following largely undesirable options have been provided by the creator's magnanimity:\n➵use -f or --force to allow overwrites/re-installs\n➵use ---self-inflicted-technical-debt to allow downgrades\n➵use ---LE-RETROGROUCH to exclusively downgrades and save the item's entry with unpinned and with a flag exclusively (In the resulting saved state, the item is unpined with a 'Le_RetroGrouch' flag)\n➵use ---retograde-stopgap to unpin, exclusively downgrades and pin the resultant version.\n➵use ---BARBAROUS to bypass virustotal security scanning, bypass certifying hashes, allow wine and allow foreign architecture  ---BARBAROUS", fmt.Errorf("adopt downgrade subverted")
			}
		} else {
			if !anyDowngradeFlag {
				if s.InState {
					return "⚠️ABORTION ~ DOWNGRADE subverted⚠️ => To avoid these abortions going forward, the following largely undesirable options have been provided by the creator's magnanimity:\n➵use -f or --force to allow overwrites/re-installs\n➵use ---self-inflicted-technical-debt to allow downgrades\n➵use ---LE-RETROGROUCH to exclusively downgrades and save the item's entry with unpinned and with a flag exclusively (In the resulting saved state, the item is unpined with a 'Le_RetroGrouch' flag) [note that if the item is pinned, --unpin is required]\n➵use ---retograde-stopgap to unpin, exclusively downgrades and pin the resultant version.\n➵use ---BARBAROUS to bypass virustotal security scanning, bypass certifying hashes, allow wine, allow foreign architecture, and allow downgrades", fmt.Errorf("error: Downgrade subverted")
				} else {
					return "DOWNGRADE SKIPPED", fmt.Errorf("downgrade skipped")
				}
			} else {
				if s.InState {
					return fmt.Sprintf("⚠️REINSTALLED ~~> 💣DOWNGRADED💥⚠️ ( %s )", baseStr), nil
				} else {
					return fmt.Sprintf("⚠️ADOPTED + REINSTALLED ~~> 💣DOWNGRADED💥⚠️ ( %s )", baseStr), nil
				}
			}
		}
	}
}
