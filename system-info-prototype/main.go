package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

// SystemInfo holds detected system information
type SystemInfo struct {
	OS              string
	Arch            string
	Distro          string
	DistroVersion   string
	DistroCodeName  string
	PkgManagers     []string
	Compilers       []string
	Shell           string
	CPUModel        string
	CPUCores        int
	RAMGB           int
	User            string
	Home            string
	InstallPrefix   string
	InstallBin      string
	InstallLib      string
	InstallShare    string
	Global          bool
}

// detectOS returns the operating system
func detectOS() string {
	return runtime.GOOS
}

// detectArch returns the architecture
func detectArch() string {
	return runtime.GOARCH
}

// detectDistro returns the Linux distribution info
func detectDistro() (name, version, codeName string) {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "unknown", "unknown", ""
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "ID=") {
			name = strings.Trim(strings.TrimPrefix(line, "ID="), "\"")
		}
		if strings.HasPrefix(line, "VERSION_ID=") {
			version = strings.Trim(strings.TrimPrefix(line, "VERSION_ID="), "\"")
		}
		if strings.HasPrefix(line, "VERSION_CODENAME=") {
			codeName = strings.Trim(strings.TrimPrefix(line, "VERSION_CODENAME="), "\"")
		}
	}

	if name == "" {
		name = "unknown"
	}
	if version == "" {
		version = "unknown"
	}

	return name, version, codeName
}

// detectPkgManagers returns available package managers
func detectPkgManagers() []string {
	managers := []string{
		"gh-pt", "mise", "cargo", "go", "micromamba", "uv", "pnpm",
		"bun", "yarn", "dotnet", "pkgx", "apt", "snap", "flatpak",
		"gearlever", "npm", "pip", "pipx", "deno", "poetry",
	}

	var available []string
	for _, mgr := range managers {
		if _, err := exec.LookPath(mgr); err == nil {
			available = append(available, mgr)
		}
	}
	return available
}

// detectCompilers returns available compilers with versions
func detectCompilers() []string {
	compilers := []struct {
		name       string
		versionCmd string
		parseFunc  func(string) string
	}{
		{"gcc", "gcc -dumpfullversion", nil},
		{"clang", "clang -dumpversion", nil},
		{"go", "go version", func(s string) string {
			// "go version go1.27.1 linux/amd64" -> "1.27.1"
			parts := strings.Fields(s)
			if len(parts) >= 3 {
				// Strip "go" prefix if present
				version := parts[2]
				if strings.HasPrefix(version, "go") {
					version = strings.TrimPrefix(version, "go")
				}
				return version
			}
			return strings.TrimSpace(s)
		}},
		{"rustc", "rustc --version", func(s string) string {
			// "rustc 1.98.1 (48a229cea 2026-09-01)" -> "1.98.1"
			parts := strings.Fields(s)
			if len(parts) >= 2 {
				return parts[1]
			}
			return strings.TrimSpace(s)
		}},
		{"cmake", "cmake --version", func(s string) string {
			// "cmake version 4.2.3" -> "4.2.3"
			parts := strings.Fields(s)
			if len(parts) >= 3 {
				return parts[2]
			}
			return strings.TrimSpace(s)
		}},
		{"ninja", "ninja --version", func(s string) string {
			// "1.11.1" -> "1.11.1"
			return strings.TrimSpace(s)
		}},
		{"bazel", "bazel --version", func(s string) string {
			// "bazel 7.0.0" -> "7.0.0"
			parts := strings.Fields(s)
			if len(parts) >= 2 {
				return parts[1]
			}
			return strings.TrimSpace(s)
		}},
		{"make", "make --version", func(s string) string {
			// "GNU Make 4.4.1" -> "4.4.1"
			parts := strings.Fields(s)
			if len(parts) >= 3 {
				return parts[2]
			}
			return strings.TrimSpace(s)
		}},
	}

	var available []string
	for _, comp := range compilers {
		if _, err := exec.LookPath(comp.name); err == nil {
			out, err := exec.Command("sh", "-c", comp.versionCmd).Output()
			if err == nil {
				lines := strings.Split(string(out), "\n")
				if len(lines) > 0 {
					version := strings.TrimSpace(lines[0])
					if comp.parseFunc != nil {
						version = comp.parseFunc(version)
					}
					available = append(available, fmt.Sprintf("%s-%s", comp.name, version))
				}
			}
		}
	}
	return available
}

// detectShell returns the current shell
func detectShell() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return "unknown"
	}
	return filepath.Base(shell)
}

// getCPUModel returns the CPU model name
func getCPUModel() string {
	out, err := exec.Command("lscpu").Output()
	if err != nil {
		return "unknown"
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, "Model name:") {
			parts := strings.Split(line, ":")
			if len(parts) > 1 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return "unknown"
}

// getRAMGB returns total RAM in GB
func getRAMGB() int {
	var info syscall.Sysinfo_t
	err := syscall.Sysinfo(&info)
	if err != nil {
		return 0
	}
	// Total RAM in bytes, convert to GB
	totalRAM := uint64(info.Totalram) * uint64(info.Unit)
	return int(totalRAM / (1024 * 1024 * 1024))
}

// getInstallPaths returns installation paths based on global flag
func getInstallPaths(global bool) (prefix, bin, lib, share string) {
	if global {
		prefix = "/usr/local"
	} else {
		home := os.Getenv("HOME")
		if home == "" {
			home = os.Getenv("USERPROFILE")
		}
		xdgDataHome := os.Getenv("XDG_DATA_HOME")
		if xdgDataHome == "" {
			xdgDataHome = filepath.Join(home, ".local", "share")
		}
		prefix = filepath.Dir(xdgDataHome)
	}

	bin = filepath.Join(prefix, "bin")
	lib = filepath.Join(prefix, "lib")
	share = filepath.Join(prefix, "share")

	return prefix, bin, lib, share
}

// getTargetBaseDirKeys returns available directory names in target base
func getTargetBaseDirKeys(baseDir string) []string {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return []string{}
	}

	var keys []string
	for _, entry := range entries {
		if entry.IsDir() {
			keys = append(keys, entry.Name())
		}
	}
	return keys
}

func main() {
	globalFlag := flag.Bool("g", false, "Simulate global install")
	flag.Parse()

	info := SystemInfo{
		OS:     detectOS(),
		Arch:   detectArch(),
		Shell:  detectShell(),
		CPUCores: runtime.NumCPU(),
		RAMGB:  getRAMGB(),
		User:   os.Getenv("USER"),
		Home:   os.Getenv("HOME"),
		Global: *globalFlag,
	}

	// Detect distro (Linux only)
	if info.OS == "linux" {
		info.Distro, info.DistroVersion, info.DistroCodeName = detectDistro()
	}

	// Detect package managers and compilers
	info.PkgManagers = detectPkgManagers()
	info.Compilers = detectCompilers()

	// Get CPU model
	info.CPUModel = getCPUModel()

	// Get install paths
	info.InstallPrefix, info.InstallBin, info.InstallLib, info.InstallShare = getInstallPaths(info.Global)

	// Output system info
	fmt.Println("=== SYSTEM INFO ===")
	fmt.Printf("os: %s\n", info.OS)
	fmt.Printf("arch: %s\n", info.Arch)
	if info.OS == "linux" {
		fmt.Printf("distro: %s\n", info.Distro)
		fmt.Printf("distro_version: %s\n", info.DistroVersion)
		if info.DistroCodeName != "" {
			fmt.Printf("distro_codename: %s\n", info.DistroCodeName)
		}
	}
	fmt.Printf("pkg_managers: %s\n", strings.Join(info.PkgManagers, ","))
	fmt.Printf("compilers: %s\n", strings.Join(info.Compilers, ","))
	fmt.Printf("shell: %s\n", info.Shell)
	fmt.Printf("cpu_model: %s\n", info.CPUModel)
	fmt.Printf("cpu_cores: %d\n", info.CPUCores)
	fmt.Printf("ram_gb: %d\n", info.RAMGB)
	fmt.Printf("user: %s\n", info.User)
	fmt.Printf("home: %s\n", info.Home)
	fmt.Printf("install_prefix: %s\n", info.InstallPrefix)
	fmt.Printf("install_bin: %s\n", info.InstallBin)
	fmt.Printf("install_lib: %s\n", info.InstallLib)
	fmt.Printf("install_share: %s\n", info.InstallShare)
	fmt.Printf("global: %v\n", info.Global)

	// Show target base dir keys
	fmt.Println("\n=== TARGET BASE DIR KEYS ===")
	targetBaseDir := info.InstallPrefix
	keys := getTargetBaseDirKeys(targetBaseDir)
	if len(keys) == 0 {
		fmt.Printf("(no subdirectories found in %s)\n", targetBaseDir)
		fmt.Println("Suggested keys: bin, lib, share, include")
	} else {
		fmt.Printf("Available in %s:\n", targetBaseDir)
		for _, key := range keys {
			fmt.Printf("  %s\n", key)
		}
	}

	// Show example helper install commands
	fmt.Println("\n=== EXAMPLE HELPER INSTALL COMMANDS ===")
	fmt.Printf("gh-pt helper --install 'bin=./myapp'\n")
	fmt.Printf("gh-pt helper --install 'lib=./libfoo.so'\n")
	fmt.Printf("gh-pt helper --install 'share=./config/'\n")
}
