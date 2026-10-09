package release

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/adrg/xdg"
	"github.com/charmbracelet/log"
)

type DriverSubsystem string

const (
	DriverAuto   DriverSubsystem = "auto"
	DriverVulkan DriverSubsystem = "vulkan"
	DriverOpenCL DriverSubsystem = "opencl"
	DriverVAAPI  DriverSubsystem = "vaapi"
	DriverUdev   DriverSubsystem = "udev"
	DriverLdSo   DriverSubsystem = "ldso"
	DriverNone   DriverSubsystem = "none"

	PluginAuto  DriverSubsystem = "auto"
	PluginOBS   DriverSubsystem = "obs"
	PluginGIMP  DriverSubsystem = "gimp"
	PluginVST   DriverSubsystem = "vst"
	PluginLV2   DriverSubsystem = "lv2"
	PluginCLAP  DriverSubsystem = "clap"
	PluginAudio DriverSubsystem = "audio"
	PluginNone  DriverSubsystem = "none"
)

type DriverManifestTarget struct {
	Subsystem    DriverSubsystem
	SourceFile   string
	DestPath     string
	IsSymlink    bool
	Content      []byte
	RequiresSudo bool
	PostHook     string // e.g. "ldconfig", "udevadm"
}

func getSystemRoot() string {
	if testRoot := os.Getenv("GH_PT_TEST_SYSDIR"); testRoot != "" {
		return testRoot
	}
	return ""
}

func sysPath(relParts ...string) string {
	root := getSystemRoot()
	if root == "" {
		return "/" + filepath.Join(relParts...)
	}
	parts := append([]string{root}, relParts...)
	return filepath.Join(parts...)
}

func getVulkanDir(global bool) string {
	if global {
		return sysPath("usr", "share", "vulkan", "icd.d")
	}
	return filepath.Join(xdg.DataHome, "vulkan", "icd.d")
}

func getOpenCLDir(global bool) string {
	if global {
		return sysPath("etc", "OpenCL", "vendors")
	}
	return filepath.Join(xdg.ConfigHome, "OpenCL", "vendors")
}

func getLdsoConfDir() string {
	return sysPath("etc", "ld.so.conf.d")
}

func getUdevRulesDir() string {
	return sysPath("etc", "udev", "rules.d")
}

func getObsPluginDir(global bool) string {
	if global {
		return sysPath("usr", "share", "obs-studio", "plugins")
	}
	return filepath.Join(xdg.ConfigHome, "obs-studio", "plugins")
}

func getGimpPluginDir(global bool) string {
	if global {
		return sysPath("usr", "lib", "gimp", "2.0", "plug-ins")
	}
	return filepath.Join(xdg.ConfigHome, "GIMP", "2.10", "plug-ins")
}

func getAudioPluginDir(subsystem DriverSubsystem, global bool) string {
	home, _ := os.UserHomeDir()
	switch subsystem {
	case PluginVST:
		if global {
			return sysPath("usr", "lib", "vst3")
		}
		return filepath.Join(home, ".vst3")
	case PluginLV2:
		if global {
			return sysPath("usr", "lib", "lv2")
		}
		return filepath.Join(home, ".lv2")
	case PluginCLAP:
		if global {
			return sysPath("usr", "lib", "clap")
		}
		return filepath.Join(home, ".clap")
	default:
		return ""
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func findFilesByPattern(dir string, predicate func(path string, info os.FileInfo) bool) []string {
	var matches []string
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if predicate(p, info) {
			matches = append(matches, p)
		}
		return nil
	})
	return matches
}

func rewriteVulkanLibraryPath(raw map[string]interface{}, jsonPath, pkgDir string) map[string]interface{} {
	fixPath := func(libPath string) string {
		if filepath.IsAbs(libPath) && fileExists(libPath) {
			return libPath
		}
		relCandidate := filepath.Join(filepath.Dir(jsonPath), libPath)
		if fileExists(relCandidate) {
			abs, _ := filepath.Abs(relCandidate)
			return abs
		}
		baseName := filepath.Base(libPath)
		found := findFilesByPattern(pkgDir, func(p string, info os.FileInfo) bool {
			return !info.IsDir() && filepath.Base(p) == baseName
		})
		if len(found) > 0 {
			abs, _ := filepath.Abs(found[0])
			return abs
		}
		soFiles := findFilesByPattern(pkgDir, func(p string, info os.FileInfo) bool {
			return !info.IsDir() && strings.Contains(p, ".so")
		})
		if len(soFiles) > 0 {
			abs, _ := filepath.Abs(soFiles[0])
			return abs
		}
		abs, _ := filepath.Abs(filepath.Join(pkgDir, libPath))
		return abs
	}

	if icd, ok := raw["ICD"].(map[string]interface{}); ok {
		if lp, ok := icd["library_path"].(string); ok {
			icd["library_path"] = fixPath(lp)
		}
	}
	if layer, ok := raw["layer"].(map[string]interface{}); ok {
		if lp, ok := layer["library_path"].(string); ok {
			layer["library_path"] = fixPath(lp)
		}
	}
	return raw
}

func detectVulkanManifests(owner, repo, pkgDir string, global bool) ([]DriverManifestTarget, error) {
	var targets []DriverManifestTarget
	vulkanDir := getVulkanDir(global)

	jsonFiles := findFilesByPattern(pkgDir, func(p string, info os.FileInfo) bool {
		return !info.IsDir() && strings.HasSuffix(strings.ToLower(p), ".json")
	})

	for _, jf := range jsonFiles {
		data, err := os.ReadFile(jf)
		if err != nil {
			continue
		}
		var raw map[string]interface{}
		if err := json.Unmarshal(data, &raw); err != nil {
			continue
		}

		isVulkan := false
		if _, ok := raw["ICD"]; ok {
			isVulkan = true
		} else if _, ok := raw["layer"]; ok {
			isVulkan = true
		} else if _, ok := raw["file_format_version"]; ok {
			base := strings.ToLower(filepath.Base(jf))
			if strings.Contains(base, "vulkan") || strings.Contains(base, "icd") {
				isVulkan = true
			}
		}

		if !isVulkan {
			continue
		}

		rewritten := rewriteVulkanLibraryPath(raw, jf, pkgDir)
		newContent, err := json.MarshalIndent(rewritten, "", "  ")
		if err != nil {
			continue
		}

		baseName := filepath.Base(jf)
		destName := fmt.Sprintf("gh-pt-%s-%s-%s", owner, repo, baseName)
		destPath := filepath.Join(vulkanDir, destName)

		targets = append(targets, DriverManifestTarget{
			Subsystem:    DriverVulkan,
			SourceFile:   jf,
			DestPath:     destPath,
			Content:      newContent,
			RequiresSudo: global,
		})
	}
	return targets, nil
}

func detectOpenCLManifests(owner, repo, pkgDir string, global bool) ([]DriverManifestTarget, error) {
	var targets []DriverManifestTarget
	openclDir := getOpenCLDir(global)

	icdFiles := findFilesByPattern(pkgDir, func(p string, info os.FileInfo) bool {
		return !info.IsDir() && strings.HasSuffix(strings.ToLower(p), ".icd")
	})

	var chosenLibPath string
	if len(icdFiles) > 0 {
		content, err := os.ReadFile(icdFiles[0])
		if err == nil {
			line := strings.TrimSpace(string(content))
			if line != "" {
				if filepath.IsAbs(line) && fileExists(line) {
					chosenLibPath = line
				} else {
					base := filepath.Base(line)
					found := findFilesByPattern(pkgDir, func(p string, info os.FileInfo) bool {
						return !info.IsDir() && filepath.Base(p) == base
					})
					if len(found) > 0 {
						abs, _ := filepath.Abs(found[0])
						chosenLibPath = abs
					}
				}
			}
		}
	}

	if chosenLibPath == "" {
		candidates := findFilesByPattern(pkgDir, func(p string, info os.FileInfo) bool {
			if info.IsDir() {
				return false
			}
			lower := strings.ToLower(filepath.Base(p))
			return strings.Contains(lower, "opencl") || strings.Contains(lower, "igdrcl") || strings.Contains(lower, "ocl")
		})
		if len(candidates) > 0 {
			abs, _ := filepath.Abs(candidates[0])
			chosenLibPath = abs
		} else {
			soFiles := findFilesByPattern(pkgDir, func(p string, info os.FileInfo) bool {
				return !info.IsDir() && strings.Contains(info.Name(), ".so")
			})
			if len(soFiles) > 0 {
				abs, _ := filepath.Abs(soFiles[0])
				chosenLibPath = abs
			}
		}
	}

	if chosenLibPath != "" {
		destName := fmt.Sprintf("gh-pt-%s-%s.icd", owner, repo)
		destPath := filepath.Join(openclDir, destName)
		content := []byte(chosenLibPath + "\n")

		targets = append(targets, DriverManifestTarget{
			Subsystem:    DriverOpenCL,
			SourceFile:   chosenLibPath,
			DestPath:     destPath,
			Content:      content,
			RequiresSudo: global,
		})
	}

	return targets, nil
}

func detectLdsoTargets(owner, repo, pkgDir string, global bool) ([]DriverManifestTarget, error) {
	var targets []DriverManifestTarget
	ldsoDir := getLdsoConfDir()

	dirsWithLibs := make(map[string]bool)
	_ = filepath.Walk(pkgDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.Contains(info.Name(), ".so") {
			absDir, _ := filepath.Abs(filepath.Dir(p))
			dirsWithLibs[absDir] = true
		}
		return nil
	})

	if len(dirsWithLibs) == 0 {
		return targets, nil
	}

	var lines []string
	for dir := range dirsWithLibs {
		lines = append(lines, dir)
	}

	destName := fmt.Sprintf("gh-pt-%s-%s.conf", owner, repo)
	destPath := filepath.Join(ldsoDir, destName)
	content := []byte(strings.Join(lines, "\n") + "\n")

	targets = append(targets, DriverManifestTarget{
		Subsystem:    DriverLdSo,
		DestPath:     destPath,
		Content:      content,
		RequiresSudo: true,
		PostHook:     "ldconfig",
	})

	return targets, nil
}

func detectUdevTargets(owner, repo, pkgDir string, global bool) ([]DriverManifestTarget, error) {
	var targets []DriverManifestTarget
	udevDir := getUdevRulesDir()

	ruleFiles := findFilesByPattern(pkgDir, func(p string, info os.FileInfo) bool {
		return !info.IsDir() && strings.HasSuffix(strings.ToLower(p), ".rules")
	})

	for _, rf := range ruleFiles {
		data, err := os.ReadFile(rf)
		if err != nil {
			continue
		}
		baseName := filepath.Base(rf)
		destName := fmt.Sprintf("99-gh-pt-%s-%s-%s", owner, repo, baseName)
		destPath := filepath.Join(udevDir, destName)

		targets = append(targets, DriverManifestTarget{
			Subsystem:    DriverUdev,
			SourceFile:   rf,
			DestPath:     destPath,
			Content:      data,
			RequiresSudo: true,
			PostHook:     "udevadm",
		})
	}
	return targets, nil
}

func detectOBSTargets(owner, repo, pkgDir string, global bool) ([]DriverManifestTarget, error) {
	var targets []DriverManifestTarget
	obsDir := getObsPluginDir(global)

	obsPluginDir := filepath.Join(pkgDir, "obs-plugins")
	var srcDir string
	if fileExists(obsPluginDir) {
		srcDir = obsPluginDir
	} else {
		binDir := filepath.Join(pkgDir, "bin", "64bit")
		if fileExists(binDir) {
			srcDir = pkgDir
		} else {
			obsLibs := findFilesByPattern(pkgDir, func(p string, info os.FileInfo) bool {
				return !info.IsDir() && strings.Contains(strings.ToLower(info.Name()), "obs") && strings.Contains(info.Name(), ".so")
			})
			if len(obsLibs) > 0 {
				srcDir = pkgDir
			}
		}
	}

	if srcDir != "" {
		destPath := filepath.Join(obsDir, repo)
		targets = append(targets, DriverManifestTarget{
			Subsystem:    PluginOBS,
			SourceFile:   srcDir,
			DestPath:     destPath,
			IsSymlink:    true,
			RequiresSudo: global,
		})
	}
	return targets, nil
}

func detectGIMPTargets(owner, repo, pkgDir string, global bool) ([]DriverManifestTarget, error) {
	var targets []DriverManifestTarget
	gimpDir := getGimpPluginDir(global)

	plugInsDir := filepath.Join(pkgDir, "plug-ins")
	var srcDir string
	if fileExists(plugInsDir) {
		srcDir = plugInsDir
	} else {
		srcDir = pkgDir
	}

	destPath := filepath.Join(gimpDir, repo)
	targets = append(targets, DriverManifestTarget{
		Subsystem:    PluginGIMP,
		SourceFile:   srcDir,
		DestPath:     destPath,
		IsSymlink:    true,
		RequiresSudo: global,
	})
	return targets, nil
}

func detectAudioTargets(owner, repo, pkgDir string, pluginMode string, global bool) ([]DriverManifestTarget, error) {
	var targets []DriverManifestTarget

	if pluginMode == "auto" || pluginMode == "audio" || pluginMode == "vst" {
		vstMatches := findFilesByPattern(pkgDir, func(p string, info os.FileInfo) bool {
			return strings.HasSuffix(strings.ToLower(p), ".vst3")
		})
		vstDir := getAudioPluginDir(PluginVST, global)
		for _, m := range vstMatches {
			destPath := filepath.Join(vstDir, filepath.Base(m))
			targets = append(targets, DriverManifestTarget{
				Subsystem:    PluginVST,
				SourceFile:   m,
				DestPath:     destPath,
				IsSymlink:    true,
				RequiresSudo: global,
			})
		}
	}

	if pluginMode == "auto" || pluginMode == "audio" || pluginMode == "lv2" {
		lv2Matches := findFilesByPattern(pkgDir, func(p string, info os.FileInfo) bool {
			return strings.HasSuffix(strings.ToLower(p), ".lv2")
		})
		lv2Dir := getAudioPluginDir(PluginLV2, global)
		for _, m := range lv2Matches {
			destPath := filepath.Join(lv2Dir, filepath.Base(m))
			targets = append(targets, DriverManifestTarget{
				Subsystem:    PluginLV2,
				SourceFile:   m,
				DestPath:     destPath,
				IsSymlink:    true,
				RequiresSudo: global,
			})
		}
	}

	if pluginMode == "auto" || pluginMode == "audio" || pluginMode == "clap" {
		clapMatches := findFilesByPattern(pkgDir, func(p string, info os.FileInfo) bool {
			return strings.HasSuffix(strings.ToLower(p), ".clap")
		})
		clapDir := getAudioPluginDir(PluginCLAP, global)
		for _, m := range clapMatches {
			destPath := filepath.Join(clapDir, filepath.Base(m))
			targets = append(targets, DriverManifestTarget{
				Subsystem:    PluginCLAP,
				SourceFile:   m,
				DestPath:     destPath,
				IsSymlink:    true,
				RequiresSudo: global,
			})
		}
	}

	return targets, nil
}

func DetectDriverTargets(owner, repo, pkgDir, driverMode string, global bool) ([]DriverManifestTarget, error) {
	mode := strings.ToLower(strings.TrimSpace(driverMode))
	if mode == "" || mode == "none" {
		return nil, nil
	}

	var targets []DriverManifestTarget

	switch mode {
	case "vulkan":
		return detectVulkanManifests(owner, repo, pkgDir, global)
	case "opencl":
		return detectOpenCLManifests(owner, repo, pkgDir, global)
	case "ldso":
		return detectLdsoTargets(owner, repo, pkgDir, global)
	case "udev":
		return detectUdevTargets(owner, repo, pkgDir, global)
	case "auto":
		vk, _ := detectVulkanManifests(owner, repo, pkgDir, global)
		targets = append(targets, vk...)

		ocl, _ := detectOpenCLManifests(owner, repo, pkgDir, global)
		targets = append(targets, ocl...)

		ud, _ := detectUdevTargets(owner, repo, pkgDir, global)
		targets = append(targets, ud...)

		if global && len(vk) == 0 && len(ocl) == 0 {
			ld, _ := detectLdsoTargets(owner, repo, pkgDir, global)
			targets = append(targets, ld...)
		}
	}

	return targets, nil
}

func DetectPluginTargets(owner, repo, pkgDir, pluginMode string, global bool) ([]DriverManifestTarget, error) {
	mode := strings.ToLower(strings.TrimSpace(pluginMode))
	if mode == "" || mode == "none" {
		return nil, nil
	}

	var targets []DriverManifestTarget

	switch mode {
	case "obs":
		return detectOBSTargets(owner, repo, pkgDir, global)
	case "gimp":
		return detectGIMPTargets(owner, repo, pkgDir, global)
	case "vst", "lv2", "clap", "audio":
		return detectAudioTargets(owner, repo, pkgDir, mode, global)
	case "auto":
		obs, _ := detectOBSTargets(owner, repo, pkgDir, global)
		targets = append(targets, obs...)

		gimp, _ := detectGIMPTargets(owner, repo, pkgDir, global)
		targets = append(targets, gimp...)

		audio, _ := detectAudioTargets(owner, repo, pkgDir, "auto", global)
		targets = append(targets, audio...)
	}

	return targets, nil
}

func writePrivilegedFile(destPath string, content []byte, perm os.FileMode, requiresSudo bool) error {
	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0755); err != nil && os.IsPermission(err) {
		if requiresSudo {
			if os.Geteuid() == 0 {
				_ = execCommand("mkdir", "-p", dir).Run()
			} else {
				_ = execCommand("sudo", "mkdir", "-p", dir).Run()
			}
		} else {
			return err
		}
	}

	if err := os.WriteFile(destPath, content, perm); err == nil {
		return nil
	} else if !os.IsPermission(err) || !requiresSudo {
		return err
	}

	tmpFile, err := os.CreateTemp("", "gh-pt-driver-*")
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()
	defer os.Remove(tmpName)

	if _, err := tmpFile.Write(content); err != nil {
		_ = tmpFile.Close()
		return err
	}
	_ = tmpFile.Close()

	var cmd *exec.Cmd
	if os.Geteuid() == 0 {
		cmd = execCommand("install", "-m", fmt.Sprintf("%04o", perm), tmpName, destPath)
	} else {
		cmd = execCommand("sudo", "install", "-m", fmt.Sprintf("%04o", perm), tmpName, destPath)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		prefix := "sudo install"
		if os.Geteuid() == 0 {
			prefix = "install"
		}
		return fmt.Errorf("%s %s failed: %w (%s)", prefix, destPath, err, string(out))
	}
	return nil
}

func (r *GithubRelease) DeployDriverAndPluginManifests(pkgDir string) ([]string, error) {
	if r.CliParams == nil {
		return nil, nil
	}

	driverMode := strings.ToLower(strings.TrimSpace(r.CliParams.Driver))
	pluginMode := strings.ToLower(strings.TrimSpace(r.CliParams.Plugin))

	if (driverMode == "" || driverMode == "none") && (pluginMode == "" || pluginMode == "none") {
		return nil, nil
	}

	if runtime.GOOS != "linux" {
		log.Warn("--driver and --plugin manifest registration is only supported on Linux", "os", runtime.GOOS)
		return nil, nil
	}

	parts := strings.Split(r.CliParams.Repository, "/")
	var owner, repo string
	if len(parts) >= 2 {
		owner = parts[0]
		repo = parts[1]
	} else if len(parts) == 1 {
		repo = parts[0]
	}

	var targets []DriverManifestTarget
	if driverMode != "" && driverMode != "none" {
		dTargets, err := DetectDriverTargets(owner, repo, pkgDir, driverMode, r.CliParams.Global)
		if err != nil {
			log.Warn("failed detecting driver targets", "error", err)
		} else {
			targets = append(targets, dTargets...)
		}
	}

	if pluginMode != "" && pluginMode != "none" {
		pTargets, err := DetectPluginTargets(owner, repo, pkgDir, pluginMode, r.CliParams.Global)
		if err != nil {
			log.Warn("failed detecting plugin targets", "error", err)
		} else {
			targets = append(targets, pTargets...)
		}
	}

	if len(targets) == 0 {
		log.Debug("no driver or plugin manifests detected", "driver", driverMode, "plugin", pluginMode)
		return nil, nil
	}

	var installed []string
	var needLdconfig, needUdevadm bool

	for _, target := range targets {
		if r.CliParams.DryRun {
			log.Infof("[dry-run] Would deploy %s manifest -> %s", target.Subsystem, target.DestPath)
			installed = append(installed, target.DestPath)
			continue
		}

		destDir := filepath.Dir(target.DestPath)
		if err := os.MkdirAll(destDir, 0755); err != nil {
			if target.RequiresSudo {
				if os.Geteuid() == 0 {
					_ = execCommand("mkdir", "-p", destDir).Run()
				} else {
					_ = execCommand("sudo", "mkdir", "-p", destDir).Run()
				}
			} else {
				log.Warn("failed creating directory", "dir", destDir, "error", err)
				continue
			}
		}

		if target.IsSymlink {
			if _, statErr := os.Lstat(target.DestPath); statErr == nil {
				if err := os.Remove(target.DestPath); err != nil && target.RequiresSudo {
					if os.Geteuid() == 0 {
						_ = execCommand("rm", "-f", target.DestPath).Run()
					} else {
						_ = execCommand("sudo", "rm", "-f", target.DestPath).Run()
					}
				}
			}
			if err := createSymlinkAtomic(target.SourceFile, target.DestPath); err != nil {
				if target.RequiresSudo {
					var cmd *exec.Cmd
					if os.Geteuid() == 0 {
						cmd = execCommand("ln", "-sfn", target.SourceFile, target.DestPath)
					} else {
						cmd = execCommand("sudo", "ln", "-sfn", target.SourceFile, target.DestPath)
					}
					if err := cmd.Run(); err != nil {
						log.Warn("failed creating privileged symlink", "src", target.SourceFile, "dest", target.DestPath, "error", err)
						continue
					}
				} else {
					log.Warn("failed creating symlink", "src", target.SourceFile, "dest", target.DestPath, "error", err)
					continue
				}
			}
			log.Infof("Registered %s plugin symlink: %s -> %s", target.Subsystem, target.DestPath, target.SourceFile)
			installed = append(installed, target.DestPath)
		} else if len(target.Content) > 0 {
			if err := writePrivilegedFile(target.DestPath, target.Content, 0644, target.RequiresSudo); err != nil {
				log.Warn("failed writing manifest", "path", target.DestPath, "error", err)
				continue
			}
			log.Infof("Deployed %s manifest: %s", target.Subsystem, target.DestPath)
			installed = append(installed, target.DestPath)
		}

		if target.PostHook == "ldconfig" {
			needLdconfig = true
		} else if target.PostHook == "udevadm" {
			needUdevadm = true
		}
	}

	if !r.CliParams.DryRun {
		if needLdconfig {
			log.Info("executing ldconfig for updated library paths")
			if os.Geteuid() == 0 {
				_ = execCommand("ldconfig").Run()
			} else {
				_ = execCommand("sudo", "ldconfig").Run()
			}
		}
		if needUdevadm {
			log.Info("reloading udev rules")
			if os.Geteuid() == 0 {
				_ = execCommand("udevadm", "control", "--reload-rules").Run()
			} else {
				_ = execCommand("sudo", "udevadm", "control", "--reload-rules").Run()
			}
			if os.Geteuid() == 0 {
				_ = execCommand("udevadm", "trigger").Run()
			} else {
				_ = execCommand("sudo", "udevadm", "trigger").Run()
			}
		}
	}

	return installed, nil
}
