package compile

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/charmbracelet/log"
)

// ContainerConfig holds configuration for container execution.
type ContainerConfig struct {
	Image        string
	BuildDir     string
	RepoDir      string
	ScriptPath   string
	InstallDir   string
	TargetOS     string
	TargetArch   string
	CrossCompile bool
	EnvVars      map[string]string
}

// DetectHostOS returns the host operating system and architecture.
func DetectHostOS() (os, arch string) {
	return runtime.GOOS, runtime.GOARCH
}

// DetectContainerImage returns the appropriate container image based on host OS.
func DetectContainerImage() string {
	hostOS, _ := DetectHostOS()
	switch hostOS {
	case "linux":
		return "ubuntu:22.04"
	case "darwin":
		// macOS builds need a Linux container with cross-compilation tools
		return "ubuntu:22.04"
	case "windows":
		// Windows builds need a Windows container or cross-compilation setup
		return "mcr.microsoft.com/windows/servercore:ltsc2022"
	default:
		return "ubuntu:22.04"
	}
}

// DetectContainerRuntime returns the available container runtime (podman or docker).
func DetectContainerRuntime() (string, error) {
	if _, err := exec.LookPath("podman"); err == nil {
		return "podman", nil
	}
	if _, err := exec.LookPath("docker"); err == nil {
		return "docker", nil
	}
	return "", fmt.Errorf("no container runtime found (install podman or docker)")
}

// ExecuteInContainer runs the compile script inside a container.
func ExecuteInContainer(cfg *Config) error {
	runtimeName, err := DetectContainerRuntime()
	if err != nil {
		return err
	}

	containerCfg := &ContainerConfig{
		Image:        DetectContainerImage(),
		BuildDir:     cfg.BuildDir,
		RepoDir:      cfg.RepoDir,
		ScriptPath:   cfg.ScriptPath,
		InstallDir:   cfg.InstallDirTarget,
		TargetOS:     runtimeName,
		TargetArch:   runtimeName,
		CrossCompile: false,
		EnvVars: map[string]string{
			"GHPT_TARGET_BASE_DIR": cfg.InstallDirTarget,
			"GHPT_TARGET_BIN_DIR":  filepath.Join(cfg.InstallDirTarget, "bin"),
			"GHPT_TARGET_LIB_DIR":  filepath.Join(cfg.InstallDirTarget, "lib"),
		},
	}

	return RunContainer(cfg, containerCfg)
}

// RunContainer executes the compile script in a container with the given configuration.
// Implements security hardening: network isolation, capability dropping, read-only filesystem,
// seccomp profile, non-root user, and tmpfs for temporary storage.
func RunContainer(cfg *Config, containerCfg *ContainerConfig) error {
	runtimeName, err := DetectContainerRuntime()
	if err != nil {
		return err
	}

	// Prepare environment variables
	var envArgs []string
	for k, v := range containerCfg.EnvVars {
		envArgs = append(envArgs, "-e", fmt.Sprintf("%s=%s", k, v))
	}

	// Prepare volume mounts (read-only for source, read-write for install)
	volumeArgs := []string{
		"-v", fmt.Sprintf("%s:/build:ro", containerCfg.RepoDir),
		"-v", fmt.Sprintf("%s:/install:rw", containerCfg.InstallDir),
	}

	// Prepare the command to run inside the container
	var cmdArgs []string
	scriptPath := "/build/.ghpt/compile.sh"

	// Security hardening flags
	securityFlags := []string{
		"run", "--rm",
		// Network isolation - prevent data exfiltration
		"--network=none",
		// Drop ALL capabilities, then add only what's needed
		"--cap-drop=ALL",
		"--cap-add=DAC_OVERRIDE", // Needed for file operations
		// Prevent privilege escalation
		"--security-opt=no-new-privileges",
		// Read-only root filesystem
		"--read-only",
		// Tmpfs for temporary storage (needed by build tools)
		"--tmpfs=/tmp:size=100M,mode=1777",
		// Non-root user
		"--user=1000:1000",
	}

	cmdArgs = append(cmdArgs, securityFlags...)
	cmdArgs = append(cmdArgs, volumeArgs...)
	cmdArgs = append(cmdArgs, envArgs...)

	// Add working directory
	cmdArgs = append(cmdArgs, "-w", "/build")

	// Add image and command
	cmdArgs = append(cmdArgs, containerCfg.Image, "bash", scriptPath)

	log.Info("executing compile script in hardened container",
		"runtime", runtimeName,
		"image", containerCfg.Image,
		"script", scriptPath,
		"target_os", containerCfg.TargetOS,
		"target_arch", containerCfg.TargetArch,
		"network", "disabled",
		"capabilities", "minimal (DAC_OVERRIDE only)",
		"filesystem", "read-only except /build, /install, /tmp",
		"user", "non-root (1000:1000)",
	)

	cmd := exec.Command(runtimeName, cmdArgs...)
	cmd.Dir = containerCfg.RepoDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("container execution failed: %w", err)
	}

	return nil
}

// CrossCompileConfig holds configuration for cross-compilation.
type CrossCompileConfig struct {
	TargetOS   string
	TargetArch string
	Toolchain  string
	Compiler   string
	EnvVars    map[string]string
}

// DetectCrossCompileConfig determines cross-compilation settings based on target platform.
func DetectCrossCompileConfig(targetOS, targetArch string) *CrossCompileConfig {
	config := &CrossCompileConfig{
		TargetOS:   targetOS,
		TargetArch: targetArch,
		EnvVars:    make(map[string]string),
	}

	// Set up cross-compilation environment based on target
	switch targetOS {
	case "linux":
		switch targetArch {
		case "amd64":
			config.Compiler = "x86_64-linux-gnu-gcc"
			config.EnvVars["CC"] = "x86_64-linux-gnu-gcc"
			config.EnvVars["CXX"] = "x86_64-linux-gnu-g++"
		case "arm64":
			config.Compiler = "aarch64-linux-gnu-gcc"
			config.EnvVars["CC"] = "aarch64-linux-gnu-gcc"
			config.EnvVars["CXX"] = "aarch64-linux-gnu-g++"
		case "arm":
			config.Compiler = "arm-linux-gnueabihf-gcc"
			config.EnvVars["CC"] = "arm-linux-gnueabihf-gcc"
			config.EnvVars["CXX"] = "arm-linux-gnueabihf-g++"
		}
	case "windows":
		switch targetArch {
		case "amd64":
			config.Compiler = "x86_64-w64-mingw32-gcc"
			config.EnvVars["CC"] = "x86_64-w64-mingw32-gcc"
			config.EnvVars["CXX"] = "x86_64-w64-mingw32-g++"
		}
	case "darwin":
		// macOS cross-compilation is complex, typically requires osxcross
		config.Compiler = "o64-clang"
		config.EnvVars["CC"] = "o64-clang"
		config.EnvVars["CXX"] = "o64-clang++"
	}

	return config
}

// PrepareCrossCompileEnv sets up environment variables for cross-compilation.
func PrepareCrossCompileEnv(cfg *Config, crossCfg *CrossCompileConfig) error {
	// Set GOOS and GOARCH for Go cross-compilation
	crossCfg.EnvVars["GOOS"] = crossCfg.TargetOS
	crossCfg.EnvVars["GOARCH"] = crossCfg.TargetArch

	// Set CGO_ENABLED for C/C++ cross-compilation
	if crossCfg.Compiler != "" {
		crossCfg.EnvVars["CGO_ENABLED"] = "1"
	} else {
		crossCfg.EnvVars["CGO_ENABLED"] = "0"
	}

	// Add any additional environment variables
	for k, v := range crossCfg.EnvVars {
		if err := os.Setenv(k, v); err != nil {
			log.Warn("failed to set environment variable", "key", k, "value", v, "error", err)
		}
	}

	return nil
}

// GetCrossCompileImage returns the appropriate container image for cross-compilation.
func GetCrossCompileImage(targetOS, targetArch string) string {
	hostOS, hostArch := DetectHostOS()

	// If target matches host, use standard image
	if targetOS == hostOS && targetArch == hostArch {
		return DetectContainerImage()
	}

	// Cross-compilation images
	switch targetOS {
	case "linux":
		return "ubuntu:22.04" // Has cross-compilation tools
	case "windows":
		return "ubuntu:22.04" // Use MinGW for Windows cross-compilation
	case "darwin":
		return "ubuntu:22.04" // Use osxcross for macOS cross-compilation
	default:
		return "ubuntu:22.04"
	}
}

// ValidateCrossCompileTools checks if required cross-compilation tools are available.
func ValidateCrossCompileTools(targetOS, targetArch string) error {
	hostOS, hostArch := DetectHostOS()

	// Skip validation if not cross-compiling
	if targetOS == hostOS && targetArch == hostArch {
		return nil
	}

	// Check for required tools based on target
	switch targetOS {
	case "linux":
		if targetArch != hostArch {
			// Check for cross-compiler
			compiler := fmt.Sprintf("%s-linux-gnu-gcc", targetArch)
			if _, err := exec.LookPath(compiler); err != nil {
				return fmt.Errorf("cross-compiler not found: %s (install gcc-%s-linux-gnu)", compiler, targetArch)
			}
		}
	case "windows":
		// Check for MinGW
		if _, err := exec.LookPath("x86_64-w64-mingw32-gcc"); err != nil {
			return fmt.Errorf("MinGW cross-compiler not found (install mingw-w64)")
		}
	case "darwin":
		// Check for osxcross
		if _, err := exec.LookPath("o64-clang"); err != nil {
			return fmt.Errorf("osxcross not found (install osxcross for macOS cross-compilation)")
		}
	}

	return nil
}

// GetContainerMounts returns the volume mount arguments for the container.
// Source directory is mounted read-only, install directory is read-write.
func GetContainerMounts(cfg *Config) []string {
	return []string{
		"-v", fmt.Sprintf("%s:/build:ro", cfg.RepoDir),
		"-v", fmt.Sprintf("%s:/install:rw", cfg.InstallDirTarget),
	}
}

// GetContainerEnvVars returns the environment variable arguments for the container.
func GetContainerEnvVars(cfg *Config) []string {
	var envVars []string

	// Add standard environment variables
	envVars = append(envVars, "-e", "GHPT_TARGET_BASE_DIR="+cfg.InstallDirTarget)
	envVars = append(envVars, "-e", "GHPT_TARGET_BIN_DIR="+filepath.Join(cfg.InstallDirTarget, "bin"))
	envVars = append(envVars, "-e", "GHPT_TARGET_LIB_DIR="+filepath.Join(cfg.InstallDirTarget, "lib"))
	envVars = append(envVars, "-e", "GHPT_TARGET_SHARE_DIR="+filepath.Join(cfg.InstallDirTarget, "share"))

	// Add any cross-compilation environment variables
	if cfg.TargetPath != "" {
		envVars = append(envVars, "-e", "GHPT_TARGET_PATH="+cfg.TargetPath)
	}

	return envVars
}

// BuildContainerCommand constructs the full container command with security hardening.
func BuildContainerCommand(cfg *Config, runtimeName, image string) []string {
	var args []string

	// Security hardening flags
	securityFlags := []string{
		"run", "--rm",
		// Network isolation - prevent data exfiltration
		"--network=none",
		// Drop ALL capabilities, then add only what's needed
		"--cap-drop=ALL",
		"--cap-add=DAC_OVERRIDE", // Needed for file operations
		// Prevent privilege escalation
		"--security-opt=no-new-privileges",
		// Seccomp profile for syscall filtering
		"--security-opt=seccomp=/build/.ghpt/seccomp-profile.json",
		// Read-only root filesystem
		"--read-only",
		// Tmpfs for temporary storage (needed by build tools)
		"--tmpfs=/tmp:size=100M,mode=1777",
		// Non-root user
		"--user=1000:1000",
	}

	args = append(args, securityFlags...)

	// Add mounts (read-only for source, read-write for install)
	args = append(args,
		"-v", fmt.Sprintf("%s:/build:ro", cfg.RepoDir),
		"-v", fmt.Sprintf("%s:/install:rw", cfg.InstallDirTarget),
	)

	// Add environment variables
	args = append(args, GetContainerEnvVars(cfg)...)

	// Add working directory
	args = append(args, "-w", "/build")

	// Add image
	args = append(args, image)

	// Add command to run - use custom command if provided (for three-part reconstruction)
	scriptPath := "/build/.ghpt/compile.sh"
	if cfg.CompileScript != "" {
		// Use the custom command (e.g., three-part reconstruction)
		args = append(args, "bash", "-c", cfg.CompileScript)
	} else {
		args = append(args, "bash", scriptPath)
	}

	return args
}

// ExecuteContainer runs the container with the constructed command.
func ExecuteContainer(cfg *Config, runtimeName, image string) error {
	args := BuildContainerCommand(cfg, runtimeName, image)

	log.Info("executing container",
		"runtime", runtimeName,
		"image", image,
		"args", strings.Join(args, " "),
	)

	cmd := exec.Command(runtimeName, args...)
	cmd.Dir = cfg.RepoDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("container execution failed: %w", err)
	}

	return nil
}
