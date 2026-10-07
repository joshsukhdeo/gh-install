package selector

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssetClassifier_DetectOS(t *testing.T) {
	c := NewAssetClassifier("linux", "amd64", "allow", "avx2")

	tests := []struct {
		name     string
		filename string
		expected AssetOS
	}{
		{"Linux tarball", "tool-v1.0.0-linux-amd64.tar.gz", OSLinux},
		{"Linux deb", "thorium-browser_154.0.8037.45_AVX2.deb", OSLinux},
		{"Linux rpm", "thorium-browser_154.0.8037.45_AVX2.rpm", OSLinux},
		{"Linux AppImage", "Thorium_Browser_154.0.8037.45_AVX2.AppImage", OSLinux},
		{"Ubuntu tarball", "ovms_ubuntu22_2026.4.1_python_on.tar.gz", OSLinux},
		{"RedHat tarball", "ovms_redhat_2026.4.1_python_off.tar.gz", OSLinux},
		{"Windows exe", "thorium_AVX2_mini_installer.exe", OSWindows},
		{"Windows zip", "ovms_windows_2026.4.1_python_on.zip", OSWindows},
		{"Windows msi", "app-setup.msi", OSWindows},
		{"macOS dmg", "Thorium_MacOS_ARM64.dmg", OSDarwin},
		{"macOS darwin tar", "tool_darwin_amd64.tar.gz", OSDarwin},
		{"FreeBSD tar", "app_freebsd_amd64.txz", OSFreeBSD},
		{"Android apk", "SystemWebView_arm32.apk", OSAndroid},
		{"Android named apk", "Thorium_Public_arm64.apk", OSAndroid},
		{"iOS ipa", "mobile_app.ipa", OSIOS},
		{"Raspberry Pi rpi", "linux_rpi4_arm64.tar.gz", OSRaspberryPi},
		{"Raspberry Pi raspbian", "kernel-raspbian.img", OSRaspberryPi},
		{"Multiplatform jar", "application.jar", OSMultiplat},
		{"Multiplatform universal", "bundle-all-platforms.zip", OSMultiplat},
		{"Multiplatform checksum", "SHA256SUMS", OSMultiplat},
		{"Multiplatform signature", "release.sig", OSMultiplat},
		{"Multiplatform source zip", "source.zip", OSMultiplat},
		{"Unknown asset", "unknown_format.xyz", OSUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, c.DetectOS(tt.filename))
		})
	}
}

func TestAssetClassifier_FormatOS(t *testing.T) {
	c := NewAssetClassifier("linux", "amd64", "allow", "avx2")

	// Emoji mode
	assert.Equal(t, "🐧", c.FormatOS(OSLinux, false))
	assert.Equal(t, "🪟", c.FormatOS(OSWindows, false))
	assert.Equal(t, "🍎", c.FormatOS(OSDarwin, false))
	assert.Equal(t, "😈", c.FormatOS(OSFreeBSD, false))
	assert.Equal(t, "🤖", c.FormatOS(OSAndroid, false))
	assert.Equal(t, "📱", c.FormatOS(OSIOS, false))
	assert.Equal(t, "🍓", c.FormatOS(OSRaspberryPi, false))
	assert.Equal(t, "🔀", c.FormatOS(OSMultiplat, false))
	assert.Equal(t, "❓", c.FormatOS(OSUnknown, false))

	// Plain text mode
	assert.Equal(t, "[linux]", c.FormatOS(OSLinux, true))
	assert.Equal(t, "[windows]", c.FormatOS(OSWindows, true))
	assert.Equal(t, "[darwin]", c.FormatOS(OSDarwin, true))
	assert.Equal(t, "[freebsd]", c.FormatOS(OSFreeBSD, true))
	assert.Equal(t, "[android]", c.FormatOS(OSAndroid, true))
	assert.Equal(t, "[ios]", c.FormatOS(OSIOS, true))
	assert.Equal(t, "[raspberrypi]", c.FormatOS(OSRaspberryPi, true))
	assert.Equal(t, "[multiplat]", c.FormatOS(OSMultiplat, true))
	assert.Equal(t, "[unknown]", c.FormatOS(OSUnknown, true))
}

func TestAssetClassifier_DetectSubcategory_LinuxHost(t *testing.T) {
	c := NewAssetClassifier("linux", "amd64", "allow", "avx2")

	assert.Equal(t, SubcatNative, c.DetectSubcategory("tool-linux-amd64.tar.gz"))
	assert.Equal(t, SubcatEmulated, c.DetectSubcategory("tool-linux-i386.tar.gz"))
	assert.Equal(t, SubcatForeign, c.DetectSubcategory("tool-linux-arm64.tar.gz"))
	assert.Equal(t, SubcatWine, c.DetectSubcategory("tool-windows-x64.exe"))
	assert.Equal(t, SubcatSidecar, c.DetectSubcategory("libhelper.so"))
	assert.Equal(t, SubcatSidecar, c.DetectSubcategory("assets.pak"))
	assert.Equal(t, SubcatChecksum, c.DetectSubcategory("tool.tar.gz.sha256"))
	assert.Equal(t, SubcatSignature, c.DetectSubcategory("tool.tar.gz.asc"))
	assert.Equal(t, SubcatMetadata, c.DetectSubcategory("source.zip"))
	assert.Equal(t, SubcatUnsupported, c.DetectSubcategory("SystemWebView_arm32.apk"))
	assert.Equal(t, SubcatUnsupported, c.DetectSubcategory("tool-darwin-arm64.dmg"))
}

func TestAssetClassifier_WindowsHostSuppressesWine(t *testing.T) {
	c := NewAssetClassifier("windows", "amd64", "allow", "avx2")

	// On Windows, Windows binaries are Native, not Wine
	assert.Equal(t, SubcatNative, c.DetectSubcategory("tool-windows-x64.exe"))
	// Wine is not in SubcategoryOrder on Windows
	order := c.SubcategoryOrder()
	for _, sub := range order {
		assert.NotEqual(t, SubcatWine, sub, "Windows host must not include Wine in subcategory order")
	}
}

func TestAssetClassifier_WineDefaultDisallow(t *testing.T) {
	// 1. Unsupported OS (windows, android) defaults to off
	cWin := NewAssetClassifier("windows", "amd64", "", "avx2")
	assert.Equal(t, "off", cWin.WineMode)

	cAndroid := NewAssetClassifier("android", "arm64", "", "")
	assert.Equal(t, "off", cAndroid.WineMode)

	// 2. When Wine is not installed, Linux host defaults to off
	origLookPath := lookPath
	defer func() { lookPath = origLookPath }()

	lookPath = func(file string) (string, error) {
		return "", errors.New("wine not found")
	}

	cNoWine := NewAssetClassifier("linux", "amd64", "", "avx2")
	assert.Equal(t, "off", cNoWine.WineMode)
	assert.Equal(t, SubcatUnsupported, cNoWine.DetectSubcategory("tool-windows-x64.exe"))
	item := cNoWine.ClassifyAsset("tool-windows-x64.exe", 0, nil, "")
	assert.Equal(t, CategoryCannotInstall, item.InstallCategory)

	for _, sub := range cNoWine.SubcategoryOrder() {
		assert.NotEqual(t, SubcatWine, sub)
	}

	// 3. When Wine IS installed, Linux host defaults to allow
	lookPath = func(file string) (string, error) {
		return "/usr/bin/" + file, nil
	}
	cWithWine := NewAssetClassifier("linux", "amd64", "", "avx2")
	assert.Equal(t, "allow", cWithWine.WineMode)
	assert.Equal(t, SubcatWine, cWithWine.DetectSubcategory("tool-windows-x64.exe"))
	itemWine := cWithWine.ClassifyAsset("tool-windows-x64.exe", 0, nil, "")
	assert.Equal(t, CategoryInstallableAlternative, itemWine.InstallCategory)
}

func TestAssetClassifier_WineExplicitDisallow(t *testing.T) {
	cOff := NewAssetClassifier("linux", "amd64", "off", "avx2")
	assert.Equal(t, "off", cOff.WineMode)
	assert.Equal(t, SubcatUnsupported, cOff.DetectSubcategory("tool-windows-x64.exe"))

	cDisallow := NewAssetClassifier("linux", "amd64", "disallow", "avx2")
	assert.Equal(t, "off", cDisallow.WineMode)
	assert.Equal(t, SubcatUnsupported, cDisallow.DetectSubcategory("tool-windows-x64.exe"))
}

func TestAssetClassifier_ThoriumRelease(t *testing.T) {
	c := NewAssetClassifier("linux", "amd64", "allow", "avx2")

	thoriumAssets := []string{
		"SystemWebView_arm32.apk",
		"SystemWebView_arm64.apk",
		"thorium-browser_154.0.8037.45_arm64.deb",
		"thorium-browser_154.0.8037.45_arm64.rpm",
		"thorium-browser_154.0.8037.45_arm64.zip",
		"thorium-browser_154.0.8037.45_AVX.deb",
		"thorium-browser_154.0.8037.45_AVX.rpm",
		"thorium-browser_154.0.8037.45_AVX.zip",
		"thorium-browser_154.0.8037.45_AVX2.deb",
		"thorium-browser_154.0.8037.45_AVX2.rpm",
		"thorium-browser_154.0.8037.45_AVX2.zip",
		"thorium-browser_154.0.8037.45_AVX512.deb",
		"thorium-browser_154.0.8037.45_AVX512.rpm",
		"thorium-browser_154.0.8037.45_AVX512.zip",
		"thorium-browser_154.0.8037.45_i386.deb",
		"thorium-browser_154.0.8037.45_i386.rpm",
		"thorium-browser_154.0.8037.45_i386.zip",
		"thorium-browser_154.0.8037.45_SSE3.deb",
		"thorium-browser_154.0.8037.45_SSE3.rpm",
		"thorium-browser_154.0.8037.45_SSE3.zip",
		"thorium-browser_154.0.8037.45_SSE4.deb",
		"thorium-browser_154.0.8037.45_SSE4.rpm",
		"thorium-browser_154.0.8037.45_SSE4.zip",
		"Thorium_ARM64_154.0.8037.45.zip",
		"thorium_ARM64_installer.exe",
		"Thorium_AVX2_154.0.8037.45.zip",
		"thorium_AVX2_mini_installer.exe",
		"Thorium_AVX512_154.0.8037.45.zip",
		"thorium_AVX512_mini_installer.exe",
		"Thorium_AVX_154.0.8037.45.zip",
		"thorium_AVX_mini_installer.exe",
		"Thorium_Browser_154.0.8037.45_arm64.AppImage",
		"Thorium_Browser_154.0.8037.45_AVX.AppImage",
		"Thorium_Browser_154.0.8037.45_AVX2.AppImage",
		"Thorium_Browser_154.0.8037.45_AVX512.AppImage",
		"Thorium_Browser_154.0.8037.45_SSE3.AppImage",
		"Thorium_Browser_154.0.8037.45_SSE4.AppImage",
		"Thorium_MacOS_ARM64.dmg",
		"Thorium_MacOS_x64.dmg",
		"Thorium_Public_arm32.apk",
		"Thorium_Public_arm64.apk",
		"Thorium_Shell_arm32.apk",
		"Thorium_Shell_arm64.apk",
		"Thorium_SSE3_154.0.8037.45.zip",
		"thorium_SSE3_mini_installer.exe",
		"Thorium_SSE4_154.0.8037.45.zip",
		"thorium_SSE4_mini_installer.exe",
		"Thorium_WIN32_SSE2_154.0.8037.45.zip",
		"thorium_WIN32_SSE2_mini_installer.exe",
	}

	classified := c.ClassifyRelease(thoriumAssets, nil)

	// Category 0: Will be installed by default
	defaultInstalls := classified[CategoryDefaultInstall]
	require.Len(t, defaultInstalls, 1, "exactly one asset should be selected as default install")
	assert.Contains(t, defaultInstalls[0].Name, "AVX2", "AVX2 package should be picked on host with AVX2")
	assert.Equal(t, SubcatNative, defaultInstalls[0].Subcat)

	// Category 1: Sidecars (none in thorium)
	assert.Empty(t, classified[CategorySidecarInstall])

	// Category 2: Installable alternatives
	alternatives := classified[CategoryInstallableAlternative]
	assert.NotEmpty(t, alternatives)
	for _, alt := range alternatives {
		assert.Contains(t, []ArchSubcategory{SubcatNative, SubcatEmulated, SubcatWine}, alt.Subcat)
	}

	// Category 3: Cannot install
	cannotInstall := classified[CategoryCannotInstall]
	assert.NotEmpty(t, cannotInstall)
	for _, item := range cannotInstall {
		assert.Contains(t, []ArchSubcategory{SubcatForeign, SubcatUnsupported, SubcatUnknown}, item.Subcat)
	}

	// Verify Android APKs are unsupported
	apkItem := c.ClassifyAsset("SystemWebView_arm32.apk", 0, thoriumAssets, defaultInstalls[0].Name)
	assert.Equal(t, OSAndroid, apkItem.OS)
	assert.Equal(t, SubcatUnsupported, apkItem.Subcat)
	assert.Equal(t, CategoryCannotInstall, apkItem.InstallCategory)

	// Verify macOS DMGs are unsupported on Linux
	dmgItem := c.ClassifyAsset("Thorium_MacOS_ARM64.dmg", 0, thoriumAssets, defaultInstalls[0].Name)
	assert.Equal(t, OSDarwin, dmgItem.OS)
	assert.Equal(t, SubcatUnsupported, dmgItem.Subcat)
	assert.Equal(t, CategoryCannotInstall, dmgItem.InstallCategory)
}

func TestAssetClassifier_OpenVINOModelServerRelease(t *testing.T) {
	c := NewAssetClassifier("linux", "amd64", "allow", "avx2")

	ovmsAssets := []string{
		"ovms_redhat_2026.4.1_python_off.tar.gz",
		"ovms_redhat_2026.4.1_python_off.tar.gz.sha256",
		"ovms_redhat_2026.4.1_python_on.tar.gz",
		"ovms_redhat_2026.4.1_python_on.tar.gz.sha256",
		"ovms_ubuntu22_2026.4.1_python_off.tar.gz",
		"ovms_ubuntu22_2026.4.1_python_off.tar.gz.sha256",
		"ovms_ubuntu22_2026.4.1_python_on.tar.gz",
		"ovms_ubuntu22_2026.4.1_python_on.tar.gz.sha256",
		"ovms_ubuntu24_2026.4.1_python_off.tar.gz",
		"ovms_ubuntu24_2026.4.1_python_off.tar.gz.sha256",
		"ovms_ubuntu24_2026.4.1_python_on.tar.gz",
		"ovms_ubuntu24_2026.4.1_python_on.tar.gz.sha256",
		"ovms_windows_2026.4.1_python_off.zip",
		"ovms_windows_2026.4.1_python_off.zip.sha256",
		"ovms_windows_2026.4.1_python_on.zip",
		"ovms_windows_2026.4.1_python_on.zip.sha256",
	}

	classified := c.ClassifyRelease(ovmsAssets, nil)

	// Checksums
	cannotInstall := classified[CategoryCannotInstall]
	assert.Len(t, cannotInstall, 8, "all 8 .sha256 files must be in CategoryCannotInstall")
	for _, item := range cannotInstall {
		assert.Equal(t, SubcatChecksum, item.Subcat)
	}

	// Default install picked
	defaultInstalls := classified[CategoryDefaultInstall]
	require.Len(t, defaultInstalls, 1)
	assert.Equal(t, OSLinux, defaultInstalls[0].OS)
	assert.Equal(t, SubcatNative, defaultInstalls[0].Subcat)

	// Alternatives (other tarballs + Windows zips via Wine)
	alternatives := classified[CategoryInstallableAlternative]
	assert.Len(t, alternatives, 7)

	// Test PickPrimaryDefaultAssetWithContext with Ubuntu matchers
	ubuntuMatchers := []string{"(?i)ubuntu.*amd64|(?i)ubuntu.*x86_64|(?i)ubuntu"}
	primary := c.PickPrimaryDefaultAssetWithContext("openvinotoolkit/model_server", ovmsAssets, ubuntuMatchers)
	assert.Contains(t, primary, "ubuntu", "should pick ubuntu asset when ubuntu regex matcher is provided")

	classifiedWithContext := c.ClassifyReleaseWithContext("openvinotoolkit/model_server", ovmsAssets, nil, ubuntuMatchers)
	require.Len(t, classifiedWithContext[CategoryDefaultInstall], 1)
	assert.Contains(t, classifiedWithContext[CategoryDefaultInstall][0].Name, "ubuntu")
}
