package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"

	"github.com/joshsukhdeo/gh-pt/ai"
	"github.com/joshsukhdeo/gh-pt/params"
)

func RunHelper(h *params.HelperCmd) error {
	switch {
	case h.GetSystemInfo:
		return helperGetSystemInfo(h)
	case h.GetManifest:
		return helperGetManifest()
	case h.ValidateManifest:
		return helperValidateManifest()
	case h.ValidateCompileScript:
		return helperValidateCompileScript()
	case h.ViewTargetDirs:
		return helperViewTargetDirs(h)
	case h.ViewInstalledFiles:
		return helperViewInstalledFiles(h)
	case h.Install != "":
		return helperInstall(h)
	case h.GetBodyTemplate:
		return helperGetBodyTemplate()
	case h.AppendManifest != "":
		return helperAppendManifest(h)
	case h.RemoveFromManifest != "":
		return helperRemoveFromManifest(h)
	case h.RunCompileScript:
		return helperRunCompileScript()
	default:
		return fmt.Errorf("no helper flag specified; use --help for usage")
	}
}

func resolveTargetBaseDir() string {
	if env := os.Getenv("GHPT_TARGET_BASE_DIR"); env != "" {
		return env
	}
	home := os.Getenv("HOME")
	if home == "" {
		home = os.Getenv("USERPROFILE")
	}
	if home == "" {
		home = "/tmp"
	}
	return filepath.Join(home, ".local")
}

func helperGetSystemInfo(h *params.HelperCmd) error {
	osName := runtime.GOOS
	arch := runtime.GOARCH
	shell := detectHelperShell()
	cpuModel := detectHelperCPUModel()
	cpuCores := runtime.NumCPU()
	ramGB := detectHelperRAMGB()
	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("USERNAME")
	}
	home := os.Getenv("HOME")
	if home == "" {
		home = os.Getenv("USERPROFILE")
	}

	targetBaseDir := resolveTargetBaseDir()
	global := false
	if targetBaseDir == "/usr/local" {
		global = true
	}

	installPrefix := targetBaseDir
	installBin := filepath.Join(installPrefix, "bin")
	installLib := filepath.Join(installPrefix, "lib")
	installShare := filepath.Join(installPrefix, "share")

	fmt.Printf("os: %s\n", osName)
	fmt.Printf("arch: %s\n", arch)

	if osName == "linux" {
		distro, version, codename := detectHelperDistro()
		fmt.Printf("distro: %s\n", distro)
		fmt.Printf("distro_version: %s\n", version)
		if codename != "" {
			fmt.Printf("distro_codename: %s\n", codename)
		}
	}

	pkgManagers := detectHelperPkgManagers()
	fmt.Printf("pkg_managers: %s\n", strings.Join(pkgManagers, ","))

	compilers := detectHelperCompilers()
	fmt.Printf("compilers: %s\n", strings.Join(compilers, ","))

	fmt.Printf("shell: %s\n", shell)
	fmt.Printf("cpu_model: %s\n", cpuModel)
	fmt.Printf("cpu_cores: %d\n", cpuCores)
	fmt.Printf("ram_gb: %d\n", ramGB)
	fmt.Printf("user: %s\n", user)
	fmt.Printf("home: %s\n", home)
	fmt.Printf("target_base_dir: %s\n", targetBaseDir)
	fmt.Printf("install_prefix: %s\n", installPrefix)
	fmt.Printf("install_bin: %s\n", installBin)
	fmt.Printf("install_lib: %s\n", installLib)
	fmt.Printf("install_share: %s\n", installShare)
	fmt.Printf("global: %v\n", global)

	return nil
}

func detectHelperShell() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return "unknown"
	}
	return filepath.Base(shell)
}

func detectHelperDistro() (name, version, codeName string) {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "unknown", "unknown", ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "ID="):
			name = strings.Trim(strings.TrimPrefix(line, "ID="), "\"")
		case strings.HasPrefix(line, "VERSION_ID="):
			version = strings.Trim(strings.TrimPrefix(line, "VERSION_ID="), "\"")
		case strings.HasPrefix(line, "VERSION_CODENAME="):
			codeName = strings.Trim(strings.TrimPrefix(line, "VERSION_CODENAME="), "\"")
		}
	}
	if name == "" {
		name = "unknown"
	}
	if version == "" {
		version = "unknown"
	}
	return
}

func detectHelperPkgManagers() []string {
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

func detectHelperCompilers() []string {
	compilers := []struct {
		name       string
		versionCmd string
		parseFunc  func(string) string
	}{
		{"gcc", "gcc -dumpfullversion", nil},
		{"clang", "clang -dumpversion", nil},
		{"go", "go version", func(s string) string {
			parts := strings.Fields(s)
			if len(parts) >= 3 {
				return strings.TrimPrefix(parts[2], "go")
			}
			return strings.TrimSpace(s)
		}},
		{"rustc", "rustc --version", func(s string) string {
			parts := strings.Fields(s)
			if len(parts) >= 2 {
				return parts[1]
			}
			return strings.TrimSpace(s)
		}},
		{"cmake", "cmake --version", func(s string) string {
			parts := strings.Fields(s)
			if len(parts) >= 3 {
				return parts[2]
			}
			return strings.TrimSpace(s)
		}},
		{"ninja", "ninja --version", nil},
		{"bazel", "bazel --version", func(s string) string {
			parts := strings.Fields(s)
			if len(parts) >= 2 {
				return parts[1]
			}
			return strings.TrimSpace(s)
		}},
		{"make", "make --version", func(s string) string {
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

func detectHelperCPUModel() string {
	if runtime.GOOS != "linux" {
		return "unknown"
	}
	out, err := exec.Command("lscpu").Output()
	if err != nil {
		return "unknown"
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "Model name:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) > 1 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return "unknown"
}

func detectHelperRAMGB() int {
	if runtime.GOOS != "linux" {
		return 0
	}
	var info syscall.Sysinfo_t
	if err := syscall.Sysinfo(&info); err != nil {
		return 0
	}
	totalRAM := uint64(info.Totalram) * uint64(info.Unit)
	return int(totalRAM / (1024 * 1024 * 1024))
}

func helperGetManifest() error {
	repoPath := os.Getenv("GHPT_REPO_PATH")
	if repoPath == "" {
		return fmt.Errorf("GHPT_REPO_PATH not set")
	}
	manifestPath := filepath.Join(repoPath, ".ghpt", "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("cannot read manifest: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

func helperValidateManifest() error {
	repoPath := os.Getenv("GHPT_REPO_PATH")
	if repoPath == "" {
		return fmt.Errorf("GHPT_REPO_PATH not set")
	}
	manifestPath := filepath.Join(repoPath, ".ghpt", "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("cannot read manifest: %w", err)
	}

	var m ai.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("manifest validation failed: invalid JSON: %w", err)
	}

	validManagers := map[string]bool{
		"apt": true, "dnf": true, "pacman": true, "brew": true,
		"apk": true, "zypper": true, "vcpkg": true, "cargo": true,
		"go": true, "pip": true, "npm": true, "pnpm": true,
		"gh-pt": true, "mise": true, "uv": true, "gem": true,
	}

	for i, dep := range m.Dependencies {
		if dep.Name == "" {
			return fmt.Errorf("manifest validation failed: dependency[%d] missing name", i)
		}
		mgr := dep.GetResolver()
		if mgr != "" && !validManagers[mgr] {
			return fmt.Errorf("manifest validation failed: unknown package manager '%s'", mgr)
		}
	}

	fmt.Println("manifest.json is valid")
	return nil
}

func helperValidateCompileScript() error {
	repoPath := os.Getenv("GHPT_REPO_PATH")
	if repoPath == "" {
		return fmt.Errorf("GHPT_REPO_PATH not set")
	}
	// Validate body.sh (what AI created), not compile.sh (reconstructed at runtime)
	scriptPath := filepath.Join(repoPath, ".ghpt", "body.sh")
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		return fmt.Errorf("cannot read body.sh: %w", err)
	}

	forbidden := []struct {
		pattern string
		desc    string
	}{
		{`apt(-get)?\s+install`, "package manager call: apt install"},
		{`dnf\s+install`, "package manager call: dnf install"},
		{`pacman\s+-S`, "package manager call: pacman -S"},
		{`brew\s+install`, "package manager call: brew install"},
		{`cargo\s+install`, "package manager call: cargo install"},
		{`go\s+install`, "package manager call: go install"},
		{`pip\s+install`, "package manager call: pip install"},
		{`npm\s+install`, "package manager call: npm install"},
		{`/usr/local/`, "direct system write: /usr/local/"},
		{`/usr/bin/`, "direct system write: /usr/bin/"},
		{`/usr/lib/`, "direct system write: /usr/lib/"},
		{`/opt/`, "direct system write: /opt/"},
		{`\bsudo\b`, "privilege escalation: sudo"},
		{`/etc/`, "system file modification: /etc/"},
		{`/var/`, "system file modification: /var/"},
	}

	content := string(data)
	for _, f := range forbidden {
		matched, _ := regexp.MatchString(f.pattern, content)
		if matched {
			return fmt.Errorf("forbidden pattern in body.sh: %s", f.desc)
		}
	}

	fmt.Println("body.sh validation passed")
	return nil
}

func helperViewTargetDirs(h *params.HelperCmd) error {
	baseDir := resolveTargetBaseDir()
	return printDirTree(baseDir, 0)
}

func printDirTree(dir string, depth int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	if depth == 0 {
		fmt.Printf("%s/\n", dir)
	}

	subdirs := []fs.DirEntry{}
	files := []fs.DirEntry{}
	for _, e := range entries {
		if e.IsDir() {
			subdirs = append(subdirs, e)
		} else {
			files = append(files, e)
		}
	}

	all := append(subdirs, files...)
	for i, entry := range all {
		prefix := strings.Repeat("│   ", depth)
		connector := "├── "
		if i == len(all)-1 {
			connector = "└── "
		}

		if entry.IsDir() {
			fmt.Printf("%s%s%s/\n", prefix, connector, entry.Name())
			childPath := filepath.Join(dir, entry.Name())
			childEntries, err := os.ReadDir(childPath)
			if err == nil && len(childEntries) > 0 {
				printDirTreeRecursive(childPath, depth+1)
			}
		} else {
			fmt.Printf("%s%s%s\n", prefix, connector, entry.Name())
		}
	}
	return nil
}

func printDirTreeRecursive(dir string, depth int) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		return
	}

	for i, entry := range entries {
		prefix := strings.Repeat("│   ", depth)
		connector := "├── "
		if i == len(entries)-1 {
			connector = "└── "
		}

		if entry.IsDir() {
			fmt.Printf("%s%s%s/\n", prefix, connector, entry.Name())
			childPath := filepath.Join(dir, entry.Name())
			printDirTreeRecursive(childPath, depth+1)
		} else {
			fmt.Printf("%s%s%s\n", prefix, connector, entry.Name())
		}
	}
}

func helperViewInstalledFiles(h *params.HelperCmd) error {
	baseDir := resolveTargetBaseDir()
	return filepath.WalkDir(baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			fmt.Println(path)
		}
		return nil
	})
}

func helperInstall(h *params.HelperCmd) error {
	parts := strings.SplitN(h.Install, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid --install format: expected dirname=source, got %q", h.Install)
	}
	dirname := parts[0]
	source := parts[1]

	targetBaseDir := resolveTargetBaseDir()
	destDir := filepath.Join(targetBaseDir, dirname)

	resolvedDest, err := filepath.Abs(destDir)
	if err != nil {
		return fmt.Errorf("cannot resolve destination: %w", err)
	}
	resolvedBase, err := filepath.Abs(targetBaseDir)
	if err != nil {
		return fmt.Errorf("cannot resolve base dir: %w", err)
	}
	if !strings.HasPrefix(resolvedDest, resolvedBase+string(filepath.Separator)) && resolvedDest != resolvedBase {
		return fmt.Errorf("destination %q is outside target base dir %q", destDir, targetBaseDir)
	}

	if _, err := os.Stat(source); err != nil {
		return fmt.Errorf("source %q does not exist: %w", source, err)
	}

	if err := os.MkdirAll(filepath.Dir(resolvedDest), 0755); err != nil {
		return fmt.Errorf("cannot create parent directory: %w", err)
	}

	srcInfo, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("cannot stat source: %w", err)
	}

	if srcInfo.IsDir() {
		if err := helperCopyDir(source, resolvedDest); err != nil {
			return fmt.Errorf("cannot copy directory: %w", err)
		}
	} else {
		if err := helperCopyFile(source, resolvedDest); err != nil {
			return fmt.Errorf("cannot copy file: %w", err)
		}
	}

	fmt.Printf("installed: %s -> %s\n", source, resolvedDest)
	return nil
}

func helperCopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	srcInfo, err := os.Stat(src)
	if err != nil {
		return nil
	}
	return os.Chmod(dst, srcInfo.Mode())
}

func helperCopyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := helperCopyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := helperCopyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func helperGetBodyTemplate() error {
	fmt.Print(ai.CompileScriptTemplate)
	return nil
}

func helperAppendManifest(h *params.HelperCmd) error {
	repoPath := os.Getenv("GHPT_REPO_PATH")
	if repoPath == "" {
		return fmt.Errorf("GHPT_REPO_PATH not set")
	}
	manifestPath := filepath.Join(repoPath, ".ghpt", "manifest.json")

	var m ai.Manifest
	if data, err := os.ReadFile(manifestPath); err == nil {
		_ = json.Unmarshal(data, &m)
	}

	entries := strings.Split(h.AppendManifest, ",")
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		eqIdx := strings.Index(entry, "=")
		if eqIdx < 0 {
			return fmt.Errorf("invalid format: %q (expected manager=pkg@version)", entry)
		}
		manager := entry[:eqIdx]
		pkgSpec := entry[eqIdx+1:]

		pkgName := pkgSpec
		version := ""
		if atIdx := strings.Index(pkgSpec, "@"); atIdx >= 0 {
			pkgName = pkgSpec[:atIdx]
			version = pkgSpec[atIdx+1:]
		}

		m.Dependencies = append(m.Dependencies, ai.Dependency{
			Name:    pkgName,
			Manager: manager,
			Version: version,
		})
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot serialize manifest: %w", err)
	}
	if err := os.WriteFile(manifestPath, data, 0644); err != nil {
		return fmt.Errorf("cannot write manifest: %w", err)
	}

	fmt.Println("manifest updated")
	return nil
}

func helperRemoveFromManifest(h *params.HelperCmd) error {
	repoPath := os.Getenv("GHPT_REPO_PATH")
	if repoPath == "" {
		return fmt.Errorf("GHPT_REPO_PATH not set")
	}
	manifestPath := filepath.Join(repoPath, ".ghpt", "manifest.json")

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("cannot read manifest: %w", err)
	}

	var m ai.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("invalid manifest JSON: %w", err)
	}

	removeSet := map[string]map[string]bool{}
	entries := strings.Split(h.RemoveFromManifest, ",")
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		eqIdx := strings.Index(entry, "=")
		if eqIdx < 0 {
			return fmt.Errorf("invalid format: %q (expected manager=pkg)", entry)
		}
		manager := entry[:eqIdx]
		pkg := entry[eqIdx+1:]
		if removeSet[manager] == nil {
			removeSet[manager] = make(map[string]bool)
		}
		removeSet[manager][pkg] = true
	}

	var filtered []ai.Dependency
	for _, dep := range m.Dependencies {
		mgr := dep.GetResolver()
		if pkgs, ok := removeSet[mgr]; ok && pkgs[dep.Name] {
			continue
		}
		filtered = append(filtered, dep)
	}
	m.Dependencies = filtered

	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot serialize manifest: %w", err)
	}
	if err := os.WriteFile(manifestPath, out, 0644); err != nil {
		return fmt.Errorf("cannot write manifest: %w", err)
	}

	fmt.Println("manifest updated")
	return nil
}

func helperRunCompileScript() error {
	repoPath := os.Getenv("GHPT_REPO_PATH")
	if repoPath == "" {
		return fmt.Errorf("GHPT_REPO_PATH not set")
	}
	bodyPath := filepath.Join(repoPath, ".ghpt", "body.sh")

	// Read body.sh (AI-generated build logic)
	bodyContent, err := os.ReadFile(bodyPath)
	if err != nil {
		return fmt.Errorf("cannot read body.sh: %w", err)
	}

	// Reconstruct compile.sh in memory: header + body.sh + footer
	header := ai.HeaderTemplate
	footer := ai.FooterTemplate

	// Get environment info for template variables
	targetBaseDir := resolveTargetBaseDir()
	// Use environment variables if set, otherwise use defaults
	repoName := os.Getenv("GHPT_REPOSITORY")
	version := os.Getenv("GHPT_VERSION")
	if repoName == "" {
		repoName = "unknown"
	}
	if version == "" {
		version = "unknown"
	}

	// Simple template variable substitution
	header = strings.ReplaceAll(header, "{{.InstallPrefix}}", targetBaseDir)
	header = strings.ReplaceAll(header, "{{.RepoPath}}", repoPath)
	header = strings.ReplaceAll(header, "{{.Repository}}", repoName)
	header = strings.ReplaceAll(header, "{{.Version}}", version)
	footer = strings.ReplaceAll(footer, "{{.RepoPath}}", repoPath)

	compileScript := header + string(bodyContent) + footer

	// Write reconstructed compile.sh to temporary file
	tmpScript := filepath.Join(repoPath, ".ghpt", "compile.sh.tmp")
	if err := os.WriteFile(tmpScript, []byte(compileScript), 0755); err != nil {
		return fmt.Errorf("cannot write temporary compile.sh: %w", err)
	}
	defer os.Remove(tmpScript)

	// Check if containerized execution is requested
	if os.Getenv("GHPT_COMPILE_CONTAINER") == "1" {
		return helperRunCompileScriptContainer(repoPath, tmpScript)
	}

	// Default: run directly with validation warning
	fmt.Fprintf(os.Stderr, "WARNING: Running compile.sh directly without container sandbox.\n")
	fmt.Fprintf(os.Stderr, "         Set GHPT_COMPILE_CONTAINER=1 to enable containerized execution.\n")
	fmt.Fprintf(os.Stderr, "         Ensure body.sh has been validated with 'ghpt helper --validate-compile-script'.\n")

	cmd := exec.Command("bash", tmpScript)
	cmd.Dir = repoPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("compile.sh failed: %w", err)
	}

	fmt.Println("compile.sh completed successfully")
	return nil
}

// helperRunCompileScriptContainer runs the compile script in a container for isolation.
// Requires podman or docker to be installed.
func helperRunCompileScriptContainer(repoPath, scriptPath string) error {
	// Check for container runtime
	containerRuntime := ""
	if _, err := exec.LookPath("podman"); err == nil {
		containerRuntime = "podman"
	} else if _, err := exec.LookPath("docker"); err == nil {
		containerRuntime = "docker"
	} else {
		return fmt.Errorf("container runtime (podman or docker) not found; install podman or docker to use GHPT_COMPILE_CONTAINER=1")
	}

	// Build container command
	// Mount repo as read-only except for build output directory
	// Run as non-root user with restricted capabilities
	args := []string{
		"run",
		"--rm",
		"--user", "1000:1000", // Non-root user
		"--cap-drop=ALL",                      // Drop all capabilities
		"--security-opt", "no-new-privileges", // Prevent privilege escalation
		"--read-only",                  // Read-only root filesystem
		"--tmpfs", "/tmp:exec,size=1g", // Writable /tmp with exec
		"--tmpfs", "/home/build:exec,size=2g", // Writable build directory
		"-v", fmt.Sprintf("%s:/src:ro", repoPath), // Source as read-only
		"-w", "/home/build", // Work in build directory
		"--network", "none", // No network access
		"ubuntu:22.04",                  // Base image
		"bash", "/src/.ghpt/compile.sh", // Command
	}

	// For docker, use --read-only with --tmpfs (works)
	// For podman, same flags work
	cmd := exec.Command(containerRuntime, args...)
	cmd.Dir = repoPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	fmt.Fprintf(os.Stderr, "Running compile.sh in %s container (isolated)...\n", containerRuntime)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("compile.sh failed in container: %w", err)
	}

	fmt.Println("compile.sh completed successfully in container")
	return nil
}
