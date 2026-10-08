package selector

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/pterm/pterm"
)

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
	SubcatNative      ArchSubcategory = "NAT"
	SubcatEmulated    ArchSubcategory = "EMU"
	SubcatWine        ArchSubcategory = "WINE"
	SubcatForeign     ArchSubcategory = "FRG"
	SubcatSidecar     ArchSubcategory = "SIDE"
	SubcatChecksum    ArchSubcategory = "SUM"
	SubcatSignature   ArchSubcategory = "SIG"
	SubcatMetadata    ArchSubcategory = "META"
	SubcatUniversal   ArchSubcategory = "UNI"
	SubcatUnsupported ArchSubcategory = "UNS"
	SubcatUnknown     ArchSubcategory = "UNK"
)

type InstallCategory int

const (
	CategoryDefaultInstall InstallCategory = iota
	CategorySidecarInstall
	CategoryInstallableAlternative
	CategoryCannotInstall
)

type ClassifiedAsset struct {
	Name            string
	Size            int64
	OS              AssetOS
	Subcat          ArchSubcategory
	InstallCategory InstallCategory
}

// AssetClassifier is the universal detection and categorization engine
// for release assets across install, update, and show commands.
type AssetClassifier struct {
	HostOS       string
	HostArch     string
	WineMode     string
	HostAVXLevel string
	HostPkgMgr   string // "dpkg", "rpm", or "" for unknown
}

var (
	lookPath = exec.LookPath
)

// IsWineSupportedOS reports whether the given OS can run Wine.
func IsWineSupportedOS(os string) bool {
	switch strings.ToLower(os) {
	case "linux", "darwin", "freebsd":
		return true
	default:
		return false
	}
}

// IsWineInstalled reports whether wine or wine64 executable is installed in PATH.
func IsWineInstalled() bool {
	_, errWine := lookPath("wine")
	_, errWine64 := lookPath("wine64")
	return errWine == nil || errWine64 == nil
}

// CanUseWine reports whether Wine can be used on the target host OS and environment.
func CanUseWine(hostOS string) bool {
	if !IsWineSupportedOS(hostOS) {
		return false
	}
	return IsWineInstalled()
}

func NewAssetClassifier(hostOS, hostArch, wineMode, hostAVX string) *AssetClassifier {
	if hostOS == "" {
		hostOS = runtime.GOOS
	}
	if hostArch == "" {
		hostArch = runtime.GOARCH
	}
	if hostAVX == "" || hostAVX == "auto" {
		hostAVX = DetectHostAVXLevel()
	}

	// Resolve Wine mode:
	// For default behavior (empty, "auto", or unconfigured), check if wine can be used
	// and default to disallow ("off") for unsupported platforms or if wine is not installed.
	if wineMode == "" || wineMode == "auto" {
		if CanUseWine(hostOS) {
			wineMode = "allow"
		} else {
			wineMode = "off"
		}
	} else if wineMode == "disallow" {
		wineMode = "off"
	} else if wineMode != "off" {
		// If wine was explicitly requested, check if the platform supports it
		if !IsWineSupportedOS(hostOS) {
			wineMode = "off"
		}
	}

	return &AssetClassifier{
		HostOS:       hostOS,
		HostArch:     hostArch,
		WineMode:     wineMode,
		HostAVXLevel: hostAVX,
		HostPkgMgr:   detectHostPackageManager(),
	}
}

// detectHostPackageManager detects the host's native package manager.
// Returns "dpkg", "rpm", or "" for unknown/unsupported.
func detectHostPackageManager() string {
	if _, err := lookPath("dpkg"); err == nil {
		return "dpkg"
	}
	if _, err := lookPath("rpm"); err == nil {
		return "rpm"
	}
	return ""
}

var defaultClassifier = NewAssetClassifier("", "", "", "")

func DefaultClassifier() *AssetClassifier {
	return defaultClassifier
}

// SubcategoryOrder returns the strict subcategory evaluation/display order for the host OS.
func (c *AssetClassifier) SubcategoryOrder() []ArchSubcategory {
	if c.HostOS == "windows" || c.WineMode == "off" || c.WineMode == "disallow" {
		return []ArchSubcategory{
			SubcatNative,
			SubcatEmulated,
			SubcatForeign,
			SubcatSidecar,
			SubcatChecksum,
			SubcatSignature,
			SubcatMetadata,
			SubcatUnsupported,
			SubcatUnknown,
		}
	}
	return []ArchSubcategory{
		SubcatNative,
		SubcatEmulated,
		SubcatWine,
		SubcatForeign,
		SubcatSidecar,
		SubcatChecksum,
		SubcatSignature,
		SubcatMetadata,
		SubcatUnsupported,
		SubcatUnknown,
	}
}

// SubcategoryTitle returns the formatted human-readable title for a subcategory.
func (c *AssetClassifier) SubcategoryTitle(subcat ArchSubcategory) string {
	switch subcat {
	case SubcatNative:
		return "native [NAT]"
	case SubcatEmulated:
		return "emulated [EMU]"
	case SubcatWine:
		return "wine [WINE]"
	case SubcatForeign:
		return "foreign [FRG]"
	case SubcatSidecar:
		return "sidecar [SIDE]"
	case SubcatChecksum:
		return "checksum/hash [SUM]"
	case SubcatSignature:
		return "signature [SIG]"
	case SubcatMetadata:
		return "metadata [META]"
	case SubcatUnsupported:
		return "incompatible/unsupported [UNS]"
	case SubcatUnknown:
		return "unknown [UNK]"
	default:
		return string(subcat)
	}
}

// FormatSubcategoryHeader returns a styled subcategory header string using pterm colors.
func (c *AssetClassifier) FormatSubcategoryHeader(subcat ArchSubcategory, noColor bool) string {
	title := c.SubcategoryTitle(subcat)
	if noColor {
		return fmt.Sprintf("  %s:", title)
	}
	switch subcat {
	case SubcatNative:
		return pterm.NewStyle(pterm.FgLightGreen, pterm.Bold).Sprintf("  %s:", title)
	case SubcatEmulated:
		return pterm.NewStyle(pterm.FgCyan, pterm.Bold).Sprintf("  %s:", title)
	case SubcatWine:
		return pterm.NewStyle(pterm.FgMagenta, pterm.Bold).Sprintf("  %s:", title)
	case SubcatForeign:
		return pterm.NewStyle(pterm.FgLightRed, pterm.Bold).Sprintf("  %s:", title)
	case SubcatSidecar:
		return pterm.NewStyle(pterm.FgLightBlue, pterm.Bold).Sprintf("  %s:", title)
	case SubcatChecksum:
		return pterm.NewStyle(pterm.FgGray).Sprintf("  %s:", title)
	case SubcatSignature:
		return pterm.NewStyle(pterm.FgLightMagenta).Sprintf("  %s:", title)
	case SubcatMetadata:
		return pterm.NewStyle(pterm.FgDarkGray).Sprintf("  %s:", title)
	case SubcatUnsupported:
		return pterm.NewStyle(pterm.FgRed).Sprintf("  %s:", title)
	case SubcatUnknown:
		return pterm.NewStyle(pterm.FgYellow).Sprintf("  %s:", title)
	default:
		return fmt.Sprintf("  %s:", title)
	}
}

// DetectOS identifies the target operating system for an asset.
func (c *AssetClassifier) DetectOS(name string) AssetOS {
	lower := strings.ToLower(name)

	// 1. Android
	if strings.HasSuffix(lower, ".apk") || strings.Contains(lower, "android") {
		return OSAndroid
	}

	// 2. iOS
	if strings.HasSuffix(lower, ".ipa") || strings.Contains(lower, "ios") ||
		strings.Contains(lower, "iphone") || strings.Contains(lower, "ipad") {
		return OSIOS
	}

	// 3. Raspberry Pi
	if strings.Contains(lower, "rpi") || strings.Contains(lower, "raspberry") ||
		strings.Contains(lower, "raspbian") || strings.Contains(lower, "armbian") ||
		strings.Contains(lower, "bcm2711") || strings.Contains(lower, "bcm2837") {
		return OSRaspberryPi
	}

	// 4. macOS / Darwin
	if strings.HasSuffix(lower, ".dmg") ||
		strings.Contains(lower, "darwin") ||
		strings.Contains(lower, "macos") ||
		strings.Contains(lower, "osx") ||
		strings.Contains(lower, "apple") ||
		strings.Contains(lower, ".app.tar.gz") ||
		strings.Contains(lower, ".app.zip") ||
		strings.HasSuffix(lower, ".app") {
		return OSDarwin
	}

	// 5. Windows
	if strings.HasSuffix(lower, ".msi") ||
		strings.HasSuffix(lower, ".exe") ||
		strings.Contains(lower, "windows") ||
		strings.Contains(lower, "win64") ||
		strings.Contains(lower, "win32") ||
		strings.Contains(lower, "-win-") ||
		strings.Contains(lower, "_win_") ||
		strings.HasPrefix(lower, "win-") ||
		strings.HasPrefix(lower, "win_") ||
		strings.HasSuffix(lower, "-win.zip") ||
		strings.HasSuffix(lower, "_win.zip") {
		return OSWindows
	}

	// 6. FreeBSD
	if strings.Contains(lower, "freebsd") {
		return OSFreeBSD
	}

	// 7. Linux
	if strings.Contains(lower, "linux") ||
		strings.HasSuffix(lower, ".deb") ||
		strings.HasSuffix(lower, ".rpm") ||
		strings.HasSuffix(lower, ".appimage") ||
		strings.HasSuffix(lower, ".flatpak") ||
		strings.HasSuffix(lower, ".snap") ||
		strings.Contains(lower, "ubuntu") ||
		strings.Contains(lower, "debian") ||
		strings.Contains(lower, "redhat") ||
		strings.Contains(lower, "fedora") ||
		strings.Contains(lower, "centos") ||
		strings.Contains(lower, "archlinux") ||
		strings.Contains(lower, "alpine") ||
		strings.Contains(lower, "musl") ||
		strings.Contains(lower, "glibc") {
		return OSLinux
	}

	// 8. Multiplatform
	if strings.HasSuffix(lower, ".jar") ||
		strings.HasSuffix(lower, ".war") ||
		strings.Contains(lower, "multiplatform") ||
		strings.Contains(lower, "multiplat") ||
		strings.Contains(lower, "all-platforms") ||
		strings.Contains(lower, "py3-none-any") ||
		c.IsChecksum(name) || c.IsSignature(name) || c.IsMetadata(name) {
		return OSMultiplat
	}

	// 9. Unknown
	return OSUnknown
}

// FormatOS returns the visual representation of an OS (emoji or [os-id]).
func (c *AssetClassifier) FormatOS(os AssetOS, noIcons bool) string {
	if noIcons {
		switch os {
		case OSLinux:
			return "[linux]"
		case OSWindows:
			return "[windows]"
		case OSDarwin:
			return "[darwin]"
		case OSFreeBSD:
			return "[freebsd]"
		case OSAndroid:
			return "[android]"
		case OSIOS:
			return "[ios]"
		case OSRaspberryPi:
			return "[raspberrypi]"
		case OSMultiplat:
			return "[multiplat]"
		case OSUnknown:
			return "[unknown]"
		default:
			return fmt.Sprintf("[%s]", os)
		}
	}

	switch os {
	case OSLinux:
		return "🐧"
	case OSWindows:
		return "🪟"
	case OSDarwin:
		return "🍎"
	case OSFreeBSD:
		return "😈"
	case OSAndroid:
		return "🤖"
	case OSIOS:
		return "📱"
	case OSRaspberryPi:
		return "🍓"
	case OSMultiplat:
		return "🔀"
	case OSUnknown:
		return "❓"
	default:
		return "❓"
	}
}

// IsChecksum reports whether the asset is a checksum or hash document.
func (c *AssetClassifier) IsChecksum(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "checksum") ||
		strings.Contains(lower, "sha256") ||
		strings.Contains(lower, "sha512") ||
		strings.Contains(lower, "sha1") ||
		strings.Contains(lower, "md5") ||
		strings.Contains(lower, "hashes") ||
		strings.HasSuffix(lower, ".digest")
}

// IsSignature reports whether the asset is a cryptographic signature or cert.
func (c *AssetClassifier) IsSignature(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".sig") ||
		strings.HasSuffix(lower, ".asc") ||
		strings.HasSuffix(lower, ".pem") ||
		strings.HasSuffix(lower, ".minisig") ||
		strings.HasSuffix(lower, ".pub") ||
		strings.HasSuffix(lower, ".key")
}

// IsMetadata reports whether the asset is documentation, source archive, or metadata.
func (c *AssetClassifier) IsMetadata(name string) bool {
	lower := strings.ToLower(name)
	if strings.Contains(lower, "source") && (strings.HasSuffix(lower, ".zip") || strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz")) {
		return true
	}
	if strings.HasSuffix(lower, "-src.zip") || strings.HasSuffix(lower, "-src.tar.gz") || strings.HasSuffix(lower, ".src.tar.gz") {
		return true
	}
	if strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".txt") || strings.HasSuffix(lower, ".json") {
		if strings.Contains(lower, "license") || strings.Contains(lower, "licence") ||
			strings.Contains(lower, "readme") || strings.Contains(lower, "changelog") ||
			strings.Contains(lower, "manifest") || strings.Contains(lower, "sbom") ||
			strings.Contains(lower, "notice") || strings.Contains(lower, "copying") {
			return true
		}
	}
	return false
}

// IsLicense reports whether the asset is a license file.
func (c *AssetClassifier) IsLicense(name string) bool {
	lower := strings.ToLower(name)
	base := strings.ToLower(filepath.Base(name))
	return strings.HasPrefix(lower, "license") ||
		strings.HasPrefix(lower, "licence") ||
		strings.HasPrefix(lower, "copying") ||
		strings.HasPrefix(base, "license") ||
		strings.HasPrefix(base, "licence") ||
		strings.HasPrefix(base, "copying") ||
		strings.Contains(base, "license") ||
		strings.Contains(base, "licence")
}

// hasCompiledPlatformTokens reports whether the asset name specifies both a target OS and CPU architecture,
// indicating it is a compiled software distribution archive rather than a companion data/asset pack.
func hasCompiledPlatformTokens(lower string) bool {
	hasOS := strings.Contains(lower, "linux") || strings.Contains(lower, "darwin") ||
		strings.Contains(lower, "windows") || strings.Contains(lower, "ubuntu") ||
		strings.Contains(lower, "debian") || strings.Contains(lower, "macos") ||
		strings.Contains(lower, "freebsd") || strings.Contains(lower, "apple")
	hasArch := strings.Contains(lower, "amd64") || strings.Contains(lower, "x86_64") ||
		strings.Contains(lower, "arm64") || strings.Contains(lower, "aarch64") ||
		strings.Contains(lower, "x64") || strings.Contains(lower, "armhf") ||
		strings.Contains(lower, "i386") || strings.Contains(lower, "386") ||
		strings.Contains(lower, "riscv")
	return hasOS && hasArch
}

// IsSidecar reports whether the asset is a companion library, pack, or data resource.
func (c *AssetClassifier) IsSidecar(name string) bool {
	if c.IsLicense(name) || c.IsChecksum(name) || c.IsSignature(name) || c.IsMetadata(name) {
		return false
	}
	lower := strings.ToLower(name)

	// Filter out source code bundles
	if strings.Contains(lower, "source") && (strings.HasSuffix(lower, ".zip") || strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz")) {
		return false
	}
	if strings.HasSuffix(lower, "-src.zip") || strings.HasSuffix(lower, "-src.tar.gz") || strings.HasSuffix(lower, ".src.tar.gz") {
		return false
	}

	// Filter out complete executables/installers
	for _, ext := range []string{".exe", ".dmg", ".pkg", ".msi", ".apk", ".deb", ".rpm", ".appimage"} {
		if strings.HasSuffix(lower, ext) {
			return false
		}
	}

	if strings.HasSuffix(lower, ".so") ||
		strings.Contains(lower, ".so.") ||
		strings.HasSuffix(lower, ".pak") ||
		strings.HasSuffix(lower, ".bin") ||
		strings.HasSuffix(lower, ".dylib") ||
		strings.HasSuffix(lower, ".dll") ||
		strings.HasSuffix(lower, ".h") ||
		strings.HasSuffix(lower, ".hpp") ||
		strings.HasSuffix(lower, ".red") {
		return true
	}

	// Archives with both OS and Arch tokens are compiled app distributions, not companion packs
	if hasCompiledPlatformTokens(lower) {
		return false
	}

	for _, kw := range []string{"plugin", "data", "model", "asset"} {
		if strings.Contains(lower, kw) {
			return true
		}
	}

	return false
}

// DetectSubcategory determines the granular compatibility subcategory for an asset.
func (c *AssetClassifier) DetectSubcategory(name string) ArchSubcategory {
	if c.IsChecksum(name) {
		return SubcatChecksum
	}
	if c.IsSignature(name) {
		return SubcatSignature
	}
	if c.IsMetadata(name) {
		return SubcatMetadata
	}

	os := c.DetectOS(name)

	// Wine check: Windows binary on non-Windows host
	if c.HostOS != "windows" && os == OSWindows {
		if c.WineMode != "off" && c.WineMode != "disallow" {
			if c.IsSidecar(name) {
				return SubcatSidecar
			}
			return SubcatWine
		}
		return SubcatUnsupported
	}

	// Check if this is an incompatible target operating system
	if os != OSMultiplat && os != OSUnknown {
		switch c.HostOS {
		case "linux":
			if os != OSLinux && os != OSRaspberryPi {
				return SubcatUnsupported
			}
		case "darwin":
			if os != OSDarwin {
				return SubcatUnsupported
			}
		case "windows":
			if os != OSWindows {
				return SubcatUnsupported
			}
		case "freebsd":
			if os != OSFreeBSD {
				return SubcatUnsupported
			}
		}
	}

	// Check CPU architecture compatibility
	lower := strings.ToLower(name)

	switch c.HostArch {
	case "amd64":
		// Hardware vector extension check (AVX)
		if !IsAVXCompatible(name, c.HostAVXLevel) {
			return SubcatUnsupported
		}

		// Foreign: ARM / AArch64 / MIPS / RISC-V / PowerPC / S390
		if strings.Contains(lower, "arm64") || strings.Contains(lower, "aarch64") ||
			strings.Contains(lower, "arm") || strings.Contains(lower, "armhf") ||
			strings.Contains(lower, "armv7") || strings.Contains(lower, "riscv") ||
			strings.Contains(lower, "s390x") || strings.Contains(lower, "ppc64") {
			return SubcatForeign
		}

		// Emulated on amd64: 32-bit x86 (i386 / 386 / win32)
		if strings.Contains(lower, "386") || strings.Contains(lower, "i386") || strings.Contains(lower, "win32") {
			return SubcatEmulated
		}

		// Check for universal Linux packages (AppImage, Flatpak, Snap) - NOT native
		// These are runtime-dependent and should not be preferred over native packages
		if strings.HasSuffix(lower, ".appimage") ||
			strings.HasSuffix(lower, ".flatpak") ||
			strings.HasSuffix(lower, ".snap") {
			return SubcatUniversal
		}

		// Native AMD64 / x86_64 - check architecture AND package format for the host OS
		if strings.Contains(lower, "amd64") || strings.Contains(lower, "x86_64") || strings.Contains(lower, "x64") {
			if c.IsSidecar(name) {
				return SubcatSidecar
			}
			// Check package format matches host OS
			if c.isNativeFormatForOS(name) {
				return SubcatNative
			}
			return SubcatForeign
		}

	case "arm64":
		// Foreign: x86_64 on Linux ARM64 without explicit emulation, RISC-V, etc.
		if c.HostOS == "linux" && (strings.Contains(lower, "amd64") || strings.Contains(lower, "x86_64") || strings.Contains(lower, "x64")) {
			return SubcatForeign
		}
		if strings.Contains(lower, "riscv") || strings.Contains(lower, "s390x") || strings.Contains(lower, "ppc64") {
			return SubcatForeign
		}

		// Native ARM64 / AArch64
		if strings.Contains(lower, "arm64") || strings.Contains(lower, "aarch64") {
			if c.IsSidecar(name) {
				return SubcatSidecar
			}
			// Check package format matches host OS
			if c.isNativeFormatForOS(name) {
				return SubcatNative
			}
			return SubcatForeign
		}

		// Emulated on ARM64:
		// Darwin arm64 runs x86_64 via Rosetta 2
		if c.HostOS == "darwin" && (strings.Contains(lower, "x86_64") || strings.Contains(lower, "amd64") || strings.Contains(lower, "x64")) {
			return SubcatEmulated
		}
		// Windows arm64 runs x86_64 / x86 via Windows emulation
		if c.HostOS == "windows" && (strings.Contains(lower, "x86_64") || strings.Contains(lower, "amd64") || strings.Contains(lower, "x64") || strings.Contains(lower, "386")) {
			return SubcatEmulated
		}
		// 32-bit ARM on Linux ARM64
		if strings.Contains(lower, "armhf") || strings.Contains(lower, "armv7") || strings.Contains(lower, "arm32") {
			return SubcatEmulated
		}
	}

	// Sidecar check for architecture-agnostic or compatible files
	if c.IsSidecar(name) {
		return SubcatSidecar
	}

	// If no foreign token matched and OS is compatible, check format before defaulting to native
	if os == OSLinux && c.HostOS == "linux" {
		if c.isNativeFormatForOS(name) {
			return SubcatNative
		}
		return SubcatForeign
	}
	if os == OSDarwin && c.HostOS == "darwin" {
		if c.isNativeFormatForOS(name) {
			return SubcatNative
		}
		return SubcatForeign
	}
	if os == OSWindows && c.HostOS == "windows" {
		if c.isNativeFormatForOS(name) {
			return SubcatNative
		}
		return SubcatForeign
	}
	if os == OSMultiplat {
		return SubcatNative
	}

	return SubcatUnknown
}

// isNativePackageFormat checks if the asset's package format matches the host's package manager.
// Returns false only if the asset is explicitly an alien distro package format (e.g. .rpm on Debian/Ubuntu, .deb on Fedora/RHEL).
// Generic binaries, tarballs, and zips are native across all Linux systems.
func (c *AssetClassifier) isNativePackageFormat(name string) bool {
	lower := strings.ToLower(name)
	switch c.HostPkgMgr {
	case "dpkg":
		// Debian/Ubuntu: .rpm and Arch Linux packages are foreign
		if strings.HasSuffix(lower, ".rpm") || strings.HasSuffix(lower, ".pkg.tar.zst") || strings.HasSuffix(lower, ".pkg.tar.xz") {
			return false
		}
		return true
	case "rpm":
		// Fedora/RHEL: .deb and Arch Linux packages are foreign
		if strings.HasSuffix(lower, ".deb") || strings.HasSuffix(lower, ".pkg.tar.zst") || strings.HasSuffix(lower, ".pkg.tar.xz") {
			return false
		}
		return true
	default:
		// Unknown package manager: treat generic formats and packages as native
		return true
	}
}

// isNativeWindowsFormat checks if the asset is a native Windows executable or archive format.
func isNativeWindowsFormat(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".deb") || strings.HasSuffix(lower, ".rpm") || strings.HasSuffix(lower, ".dmg") || strings.HasSuffix(lower, ".pkg") || strings.HasSuffix(lower, ".appimage") {
		return false
	}
	return true
}

// isNativeDarwinFormat checks if the asset is a native macOS format.
func isNativeDarwinFormat(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".deb") || strings.HasSuffix(lower, ".rpm") || strings.HasSuffix(lower, ".exe") || strings.HasSuffix(lower, ".msi") || strings.HasSuffix(lower, ".appimage") {
		return false
	}
	return true
}

// isNativeFormatForOS checks if the asset's format is native for the host OS.
func (c *AssetClassifier) isNativeFormatForOS(name string) bool {
	switch c.HostOS {
	case "linux":
		return c.isNativePackageFormat(name)
	case "windows":
		return isNativeWindowsFormat(name)
	case "darwin":
		return isNativeDarwinFormat(name)
	case "freebsd":
		lower := strings.ToLower(name)
		if strings.HasSuffix(lower, ".deb") || strings.HasSuffix(lower, ".rpm") || strings.HasSuffix(lower, ".exe") || strings.HasSuffix(lower, ".dmg") {
			return false
		}
		return true
	default:
		return c.isNativePackageFormat(name) || isNativeWindowsFormat(name) || isNativeDarwinFormat(name)
	}
}

// ClassifyAsset categorizes a single asset against the full release asset list.
func (c *AssetClassifier) ClassifyAsset(name string, size int64, allAssets []string, primaryDefaultAsset string) ClassifiedAsset {
	os := c.DetectOS(name)
	subcat := c.DetectSubcategory(name)

	// If this is the primary default asset chosen for installation, it cannot be a sidecar
	if primaryDefaultAsset != "" && name == primaryDefaultAsset && subcat == SubcatSidecar {
		subcat = SubcatNative
	}

	var installCat InstallCategory

	if primaryDefaultAsset != "" && name == primaryDefaultAsset {
		installCat = CategoryDefaultInstall
	} else if subcat == SubcatChecksum || subcat == SubcatSignature || subcat == SubcatMetadata || subcat == SubcatUnsupported || subcat == SubcatUnknown || subcat == SubcatForeign {
		installCat = CategoryCannotInstall
	} else if subcat == SubcatSidecar {
		installCat = CategorySidecarInstall
	} else {
		// Valid runnable alternative (alternative instruction set, package format, Wine, or emulated)
		installCat = CategoryInstallableAlternative
	}

	return ClassifiedAsset{
		Name:            name,
		Size:            size,
		OS:              os,
		Subcat:          subcat,
		InstallCategory: installCat,
	}
}

func isPackageFormat(name string) bool {
	lower := strings.ToLower(name)
	for _, ext := range []string{".deb", ".rpm", ".appimage", ".flatpak", ".snap", ".apk", ".pkg.tar.zst", ".pkg.tar.xz", ".dmg", ".pkg", ".msi"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// BinaryMagic represents the magic header signature of an executable binary format.
type BinaryMagic int

const (
	BinaryMagicUnknown BinaryMagic = iota
	BinaryMagicELF                 // Linux/Unix executable (\x7fELF)
	BinaryMagicMachO32             // macOS 32-bit (\xfe\xed\xfa\xce)
	BinaryMagicMachO64             // macOS 64-bit (\xfe\xed\xfa\xcf)
	BinaryMagicPE                  // Windows PE/COFF (MZ)
	BinaryMagicWasm                // WebAssembly (\x00asm)
)

// DetectBinaryMagic reads the first few bytes of a file to identify its executable format.
func DetectBinaryMagic(path string) BinaryMagic {
	data, err := os.ReadFile(path)
	if err != nil || len(data) < 4 {
		return BinaryMagicUnknown
	}

	// Check for ELF (\x7fELF)
	if bytes.HasPrefix(data, []byte{0x7f, 'E', 'L', 'F'}) {
		return BinaryMagicELF
	}

	// Check for Mach-O 32-bit (\xfe\xed\xfa\xce) and 64-bit (\xfe\xed\xfa\xcf)
	if bytes.HasPrefix(data, []byte{0xfe, 0xed, 0xfa, 0xce}) {
		return BinaryMagicMachO32
	}
	if bytes.HasPrefix(data, []byte{0xfe, 0xed, 0xfa, 0xcf}) {
		return BinaryMagicMachO64
	}

	// Check for PE/COFF (MZ)
	if bytes.HasPrefix(data, []byte{'M', 'Z'}) {
		return BinaryMagicPE
	}

	// Check for WebAssembly (\x00asm)
	if bytes.HasPrefix(data, []byte{0x00, 'a', 's', 'm'}) {
		return BinaryMagicWasm
	}

	return BinaryMagicUnknown
}

// IsVerifiedBinary checks if a file at path is a verified executable binary by magic header.
// Returns true only for ELF, Mach-O, PE, or Wasm binaries.
func IsVerifiedBinary(path string) bool {
	magic := DetectBinaryMagic(path)
	return magic != BinaryMagicUnknown
}

// IsVerifiedBinaryStream checks if an io.Reader stream starts with a verified binary magic header.
// Reads only the first 4 bytes without consuming the entire stream.
func IsVerifiedBinaryStream(r io.Reader) (bool, error) {
	header := make([]byte, 4)
	n, err := io.ReadFull(r, header)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return false, err
	}
	if n < 4 {
		return false, nil
	}

	if bytes.HasPrefix(header, []byte{0x7f, 'E', 'L', 'F'}) {
		return true, nil
	}
	if bytes.HasPrefix(header, []byte{0xfe, 0xed, 0xfa, 0xce}) || bytes.HasPrefix(header, []byte{0xfe, 0xed, 0xfa, 0xcf}) {
		return true, nil
	}
	if bytes.HasPrefix(header, []byte{'M', 'Z'}) {
		return true, nil
	}
	if bytes.HasPrefix(header, []byte{0x00, 'a', 's', 'm'}) {
		return true, nil
	}

	return false, nil
}

// DetectArchiveMagic detects common archive container magic headers.
func DetectArchiveMagic(header []byte) string {
	if len(header) < 2 {
		return ""
	}
	if bytes.HasPrefix(header, []byte{0x1f, 0x8b}) {
		return "gzip"
	}
	if bytes.HasPrefix(header, []byte{'P', 'K', 0x03, 0x04}) {
		return "zip"
	}
	if bytes.HasPrefix(header, []byte{'B', 'Z', 'h'}) {
		return "bzip2"
	}
	if len(header) >= 6 && bytes.HasPrefix(header, []byte{0xfd, '7', 'z', 'X', 'Z', 0x00}) {
		return "xz"
	}
	if len(header) >= 6 && bytes.HasPrefix(header, []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}) {
		return "7z"
	}
	if len(header) >= 4 && bytes.HasPrefix(header, []byte{0x28, 0xb5, 0x2f, 0xfd}) {
		return "zstd"
	}
	return ""
}

// VerifyAssetContentConsistency checks whether a file's content magic header matches its filename extension.
// Catches spoofed file extensions, misnamed executables, and unexpected packaging.
func VerifyAssetContentConsistency(name string, header []byte) (bool, string) {
	if len(header) < 4 {
		return true, "insufficient data"
	}
	lower := strings.ToLower(name)
	arch := DetectArchiveMagic(header)

	if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
		return arch == "gzip", "expected gzip magic for tar.gz"
	}
	if strings.HasSuffix(lower, ".zip") {
		return arch == "zip", "expected zip magic for .zip"
	}
	if strings.HasSuffix(lower, ".tar.bz2") || strings.HasSuffix(lower, ".tbz2") {
		return arch == "bzip2", "expected bzip2 magic for tar.bz2"
	}
	if strings.HasSuffix(lower, ".tar.xz") || strings.HasSuffix(lower, ".txz") {
		return arch == "xz", "expected xz magic for tar.xz"
	}
	if strings.HasSuffix(lower, ".7z") {
		return arch == "7z", "expected 7z magic for .7z"
	}
	if strings.HasSuffix(lower, ".tar.zst") || strings.HasSuffix(lower, ".tzst") {
		return arch == "zstd", "expected zstd magic for tar.zst"
	}

	// For standalone executables (e.g. .exe)
	if strings.HasSuffix(lower, ".exe") {
		return bytes.HasPrefix(header, []byte{'M', 'Z'}), "expected PE/MZ header for .exe"
	}

	return true, "consistent"
}

var osReadFile = os.ReadFile

func extractPackageBase(name string) string {
	base := filepath.Base(name)
	ext := filepath.Ext(base)
	base = strings.TrimSuffix(base, ext)
	if idx := strings.Index(base, "_"); idx > 0 {
		return strings.ToLower(base[:idx])
	}
	if idx := strings.Index(base, "-"); idx > 0 {
		return strings.ToLower(base[:idx])
	}
	return strings.ToLower(base)
}

// PickDefaultAssetsWithContext identifies all assets that should be installed by default.
// Packages compete only amongst themselves for precedence (e.g. .deb > .appimage > .rpm on dpkg).
// Binaries found (standalone executable or companion CLI) are installed alongside the package by default,
// unless the binary is clearly a sidecar, checksum, or leftover dev byproduct.
func (c *AssetClassifier) PickDefaultAssetsWithContext(repo string, assets []string, regexMatchers []string) []string {
	if len(assets) == 0 {
		return nil
	}

	effectiveAVX := c.HostAVXLevel
	if effectiveAVX == "" {
		effectiveAVX = "avx"
	}

	var candidates []string
	for _, a := range assets {
		subcat := c.DetectSubcategory(a)
		if subcat == SubcatNative {
			candidates = append(candidates, a)
		}
	}

	if len(candidates) == 0 {
		for _, a := range assets {
			subcat := c.DetectSubcategory(a)
			if subcat == SubcatEmulated || (c.WineMode != "off" && c.WineMode != "disallow" && subcat == SubcatWine) {
				candidates = append(candidates, a)
			}
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	// Filter out sidecars, checksums, signatures, metadata, licenses, sources, and dev byproducts
	var validCandidates []string
	for _, cand := range candidates {
		lower := strings.ToLower(cand)
		if c.IsSidecar(cand) || c.IsChecksum(cand) || c.IsSignature(cand) || c.IsMetadata(cand) || c.IsLicense(cand) {
			continue
		}
		if strings.Contains(lower, "source") && (strings.HasSuffix(lower, ".zip") || strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz")) {
			continue
		}
		if strings.HasSuffix(lower, "-src.zip") || strings.HasSuffix(lower, "-src.tar.gz") || strings.HasSuffix(lower, ".src.tar.gz") {
			continue
		}
		validCandidates = append(validCandidates, cand)
	}

	if len(validCandidates) == 0 {
		validCandidates = candidates
	}

	// Partition into packages and binaries
	var packageCandidates []string
	var binaryCandidates []string

	for _, cand := range validCandidates {
		if isPackageFormat(cand) {
			packageCandidates = append(packageCandidates, cand)
		} else {
			binaryCandidates = append(binaryCandidates, cand)
		}
	}

	// If regex matchers are provided, filter candidates according to the regexes
	if len(regexMatchers) > 0 {
		var matchedPkgs []string
		for _, rx := range regexMatchers {
			compiled, err := regexp.Compile(rx)
			if err != nil {
				continue
			}
			for _, cand := range packageCandidates {
				if compiled.MatchString(cand) {
					matchedPkgs = append(matchedPkgs, cand)
				}
			}
			if len(matchedPkgs) > 0 {
				packageCandidates = matchedPkgs
				break
			}
		}

		var matchedBins []string
		for _, rx := range regexMatchers {
			compiled, err := regexp.Compile(rx)
			if err != nil {
				continue
			}
			for _, cand := range binaryCandidates {
				if compiled.MatchString(cand) {
					matchedBins = append(matchedBins, cand)
				}
			}
			if len(matchedBins) > 0 {
				binaryCandidates = matchedBins
				break
			}
		}
	}

	// 1. Pick the best package candidate (packages compete only amongst themselves)
	var bestPackage string
	if len(packageCandidates) > 0 {
		for _, cand := range packageCandidates {
			if strings.Contains(strings.ToLower(cand), strings.ToLower(effectiveAVX)) {
				bestPackage = cand
				break
			}
		}
		if bestPackage == "" {
			switch c.HostOS {
			case "linux":
				hasDpkg := false
				hasRpm := false
				if _, err := lookPath("dpkg"); err == nil {
					hasDpkg = true
				}
				if _, err := lookPath("rpm"); err == nil {
					hasRpm = true
				}
				var preferredExts []string
				if hasDpkg && !hasRpm {
					preferredExts = []string{".deb", ".snap", ".flatpak", ".appimage"}
				} else if hasRpm && !hasDpkg {
					preferredExts = []string{".rpm", ".snap", ".flatpak", ".appimage"}
				} else if hasDpkg && hasRpm {
					preferredExts = []string{".deb", ".rpm", ".snap", ".flatpak", ".appimage"}
				} else {
					preferredExts = []string{".appimage", ".flatpak", ".snap"}
				}
				for _, pref := range preferredExts {
					for _, cand := range packageCandidates {
						if strings.EqualFold(filepath.Ext(cand), pref) {
							bestPackage = cand
							break
						}
					}
					if bestPackage != "" {
						break
					}
				}
			case "darwin":
				for _, pref := range []string{".dmg", ".pkg"} {
					for _, cand := range packageCandidates {
						if strings.EqualFold(filepath.Ext(cand), pref) {
							bestPackage = cand
							break
						}
					}
					if bestPackage != "" {
						break
					}
				}
			case "windows":
				for _, pref := range []string{".msi", ".exe"} {
					for _, cand := range packageCandidates {
						if strings.EqualFold(filepath.Ext(cand), pref) {
							bestPackage = cand
							break
						}
					}
					if bestPackage != "" {
						break
					}
				}
			}
			if bestPackage == "" {
				bestPackage = packageCandidates[0]
			}
		}
	}

	// 2. Pick the best binary candidate (if any)
	var bestBinary string
	if len(binaryCandidates) > 0 {
		for _, cand := range binaryCandidates {
			if strings.Contains(strings.ToLower(cand), strings.ToLower(effectiveAVX)) {
				bestBinary = cand
				break
			}
		}
		if bestBinary == "" {
			// If a package is also present, only choose a binary if it is a standalone executable
			// or clearly a distinct CLI/tool (to avoid duplicate archives of the same program)
			if bestPackage != "" {
				pkgBase := extractPackageBase(bestPackage)
				for _, cand := range binaryCandidates {
					lowerCand := strings.ToLower(cand)
					ext := filepath.Ext(cand)
					// Only choose binary if it is a distinct companion CLI/tool (e.g. app-cli)
					// to avoid duplicate binary collision between package and standalone binary of the same app.
					isCompanion := strings.Contains(lowerCand, "cli") ||
						strings.Contains(lowerCand, "daemon") ||
						strings.Contains(lowerCand, "server") ||
						strings.Contains(lowerCand, "agent") ||
						strings.Contains(lowerCand, "tool")
					if !isCompanion {
						candBase := extractPackageBase(cand)
						if candBase == pkgBase {
							// Redundant duplicate binary of the same program
							continue
						}
					}
					// Standalone executable (extensionless or .exe on Windows) or companion tool
					if ext == "" || (c.HostOS == "windows" && strings.EqualFold(ext, ".exe")) || isCompanion {
						if !strings.Contains(lowerCand, "musl") {
							bestBinary = cand
							break
						} else if bestBinary == "" {
							bestBinary = cand
						}
					}
				}
			} else {
				// No package is present: pick standard binary/archive
				for _, cand := range binaryCandidates {
					lowerCand := strings.ToLower(cand)
					if !strings.Contains(lowerCand, "musl") {
						bestBinary = cand
						break
					}
				}
				if bestBinary == "" {
					bestBinary = binaryCandidates[0]
				}
			}
		}
	}

	var results []string
	if bestPackage != "" {
		results = append(results, bestPackage)
	}
	if bestBinary != "" {
		results = append(results, bestBinary)
	}

	if len(results) > 0 {
		return results
	}

	// Fallback to legacy single asset selector
	primary := c.PickPrimaryDefaultAssetWithContext(repo, assets, regexMatchers)
	if primary != "" {
		return []string{primary}
	}
	return nil
}

// PickPrimaryDefaultAsset identifies which asset from the release gh-pt will select by default.
func (c *AssetClassifier) PickPrimaryDefaultAsset(assets []string) string {
	return c.PickPrimaryDefaultAssetWithContext("", assets, nil)
}

// PickPrimaryDefaultAssetWithContext executes the core Selector engine with regex matchers
// and repository context, ensuring exact parity with gh-pt install / update asset selection.
func (c *AssetClassifier) PickPrimaryDefaultAssetWithContext(repo string, assets []string, regexMatchers []string) string {
	if len(assets) == 0 {
		return ""
	}

	// 1. If regex matchers are provided, execute the core Selector engine
	if len(regexMatchers) > 0 {
		var selectorItems []*SelectorItem
		for _, a := range assets {
			selectorItems = append(selectorItems, &SelectorItem{
				Name: a,
			})
		}
		sel := &Selector{
			Kind:             Asset,
			Items:            selectorItems,
			RegexpMatchers:   regexMatchers,
			Repository:       repo,
			Single:           true,
			OnlyFirstMatch:   true,
			AllowForeignArch: false,
			AvxLevel:         c.HostAVXLevel,
		}
		if matches, err := sel.Run(); err == nil && len(matches) > 0 {
			return matches[0].Name
		}
	}

	// 2. Fallback heuristic: Check for AVX-matched native Linux/Darwin/Windows package
	effectiveAVX := c.HostAVXLevel
	if effectiveAVX == "" {
		effectiveAVX = "avx"
	}

	var candidates []string
	for _, a := range assets {
		subcat := c.DetectSubcategory(a)
		if subcat == SubcatNative {
			candidates = append(candidates, a)
		}
	}

	if len(candidates) == 0 {
		for _, a := range assets {
			subcat := c.DetectSubcategory(a)
			if subcat == SubcatEmulated || (c.WineMode != "off" && subcat == SubcatWine) {
				candidates = append(candidates, a)
			}
		}
	}

	if len(candidates) == 0 {
		return ""
	}

	// Prefer candidate matching host AVX if present
	for _, cand := range candidates {
		lower := strings.ToLower(cand)
		if strings.Contains(lower, strings.ToLower(effectiveAVX)) {
			return cand
		}
	}

	// Prefer standard binary/package formats over general archives, respecting host package manager
	switch c.HostOS {
	case "linux":
		hasDpkg := false
		hasRpm := false
		if _, err := lookPath("dpkg"); err == nil {
			hasDpkg = true
		}
		if _, err := lookPath("rpm"); err == nil {
			hasRpm = true
		}
		var preferredExts []string
		if hasDpkg && !hasRpm {
			preferredExts = []string{".deb", ".snap", ".flatpak", ".appimage"}
		} else if hasRpm && !hasDpkg {
			preferredExts = []string{".rpm", ".snap", ".flatpak", ".appimage"}
		} else if hasDpkg && hasRpm {
			preferredExts = []string{".deb", ".rpm", ".snap", ".flatpak", ".appimage"}
		} else {
			preferredExts = []string{".appimage", ".flatpak", ".snap"}
		}
		for _, pref := range preferredExts {
			for _, cand := range candidates {
				if strings.EqualFold(filepath.Ext(cand), pref) {
					return cand
				}
			}
		}
	case "darwin":
		for _, pref := range []string{".dmg", ".pkg"} {
			for _, cand := range candidates {
				if strings.EqualFold(filepath.Ext(cand), pref) {
					return cand
				}
			}
		}
	case "windows":
		for _, pref := range []string{".exe", ".msi"} {
			for _, cand := range candidates {
				if strings.EqualFold(filepath.Ext(cand), pref) {
					return cand
				}
			}
		}
	}

	return candidates[0]
}

// ClassifyRelease takes a full list of asset names and sizes, classifies them,
// and returns them grouped by the 4 top-level categories.
func (c *AssetClassifier) ClassifyRelease(names []string, sizes []int64) map[InstallCategory][]ClassifiedAsset {
	return c.ClassifyReleaseWithContext("", names, sizes, nil)
}

// ClassifyReleaseWithContext takes repository context and regex matchers to ensure
// exact parity with gh-pt install / update asset selection.
func (c *AssetClassifier) ClassifyReleaseWithContext(repo string, names []string, sizes []int64, regexMatchers []string) map[InstallCategory][]ClassifiedAsset {
	defaultAssets := c.PickDefaultAssetsWithContext(repo, names, regexMatchers)
	defaultMap := make(map[string]bool)
	for _, da := range defaultAssets {
		defaultMap[da] = true
	}

	result := make(map[InstallCategory][]ClassifiedAsset)

	for i, name := range names {
		var sz int64
		if i < len(sizes) {
			sz = sizes[i]
		}
		item := c.ClassifyAsset(name, sz, names, "")
		if defaultMap[name] {
			item.InstallCategory = CategoryDefaultInstall
		}
		result[item.InstallCategory] = append(result[item.InstallCategory], item)
	}

	return result
}
