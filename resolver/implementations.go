package resolver

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Compile-time interface checks
var (
	_ PackageManager = (*AptManager)(nil)
	_ PackageManager = (*DnfManager)(nil)
	_ PackageManager = (*PacmanManager)(nil)
	_ PackageManager = (*MiseManager)(nil)
	_ PackageManager = (*UvManager)(nil)
	_ PackageManager = (*CargoManager)(nil)
	_ PackageManager = (*VcpkgManager)(nil)
)

// AptManager manages packages via apt-get or apt.
type AptManager struct {
	UseSudo bool
}

func NewAptManager() *AptManager {
	return &AptManager{}
}

func (m *AptManager) Name() string {
	return "apt"
}

func (m *AptManager) IsInstalled() bool {
	if _, err := lookPath("apt-get"); err == nil {
		return true
	}
	if _, err := lookPath("apt"); err == nil {
		return true
	}
	return false
}

func (m *AptManager) Install(pkgs []string) error {
	if len(pkgs) == 0 {
		return nil
	}
	bin := "apt-get"
	if _, err := lookPath("apt-get"); err != nil {
		bin = "apt"
	}
	args := append([]string{"install", "-y"}, pkgs...)
	if m.UseSudo {
		args = append([]string{bin}, args...)
		bin = "sudo"
	}

	return m.runWithRetry(bin, args...)
}

func (m *AptManager) runWithRetry(bin string, args ...string) error {
	const maxRetries = 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		cmd := execCommand(bin, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		err := cmd.Run()

		if err == nil {
			return nil
		}

		lastErr = err

		// Check for dpkg lock
		if m.isDpkgLocked(err) {
			if attempt < maxRetries-1 {
				waitTime := time.Duration(attempt+1) * 10 * time.Second
				fmt.Fprintf(os.Stderr, "dpkg lock detected, waiting %v before retry (%d/%d)...\n", waitTime, attempt+1, maxRetries)
				time.Sleep(waitTime)
				continue
			}
			return fmt.Errorf("dpkg lock could not be acquired after %d attempts: %w", maxRetries, err)
		}

		// Check for broken packages
		if m.isBrokenPackageError(err) {
			return fmt.Errorf("broken package state detected, manual intervention required: %w", err)
		}

		// For other errors, retry with exponential backoff
		if attempt < maxRetries-1 {
			waitTime := time.Duration(1<<attempt) * 2 * time.Second
			fmt.Fprintf(os.Stderr, "Install failed, retrying in %v (%d/%d): %v\n", waitTime, attempt+1, maxRetries, err)
			time.Sleep(waitTime)
			continue
		}
	}

	return fmt.Errorf("failed after %d attempts: %w", maxRetries, lastErr)
}

func (m *AptManager) isDpkgLocked(err error) bool {
	errStr := err.Error()
	return strings.Contains(errStr, "Could not get lock") ||
		strings.Contains(errStr, "dpkg lock") ||
		strings.Contains(errStr, "dpkg status database is locked") ||
		strings.Contains(errStr, "waiting for lock") ||
		strings.Contains(errStr, "apt lock")
}

func (m *AptManager) isBrokenPackageError(err error) bool {
	errStr := err.Error()
	return strings.Contains(errStr, "broken package") ||
		strings.Contains(errStr, "unmet dependencies") ||
		strings.Contains(errStr, "dpkg: error processing")
}

// DnfManager manages packages via dnf.
type DnfManager struct {
	UseSudo bool
}

func NewDnfManager() *DnfManager {
	return &DnfManager{}
}

func (m *DnfManager) Name() string {
	return "dnf"
}

func (m *DnfManager) IsInstalled() bool {
	_, err := lookPath("dnf")
	return err == nil
}

func (m *DnfManager) Install(pkgs []string) error {
	if len(pkgs) == 0 {
		return nil
	}
	bin := "dnf"
	args := append([]string{"install", "-y"}, pkgs...)
	if m.UseSudo {
		args = append([]string{bin}, args...)
		bin = "sudo"
	}

	return m.runWithRetry(bin, args...)
}

func (m *DnfManager) runWithRetry(bin string, args ...string) error {
	const maxRetries = 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		cmd := execCommand(bin, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		err := cmd.Run()

		if err == nil {
			return nil
		}

		lastErr = err

		// Check for dnf lock
		if m.isDnfLocked(err) {
			if attempt < maxRetries-1 {
				waitTime := time.Duration(attempt+1) * 10 * time.Second
				fmt.Fprintf(os.Stderr, "dnf lock detected, waiting %v before retry (%d/%d)...\n", waitTime, attempt+1, maxRetries)
				time.Sleep(waitTime)
				continue
			}
			return fmt.Errorf("dnf lock could not be acquired after %d attempts: %w", maxRetries, err)
		}

		// For other errors, retry with exponential backoff
		if attempt < maxRetries-1 {
			waitTime := time.Duration(1<<attempt) * 2 * time.Second
			fmt.Fprintf(os.Stderr, "Install failed, retrying in %v (%d/%d): %v\n", waitTime, attempt+1, maxRetries, err)
			time.Sleep(waitTime)
			continue
		}
	}

	return fmt.Errorf("failed after %d attempts: %w", maxRetries, lastErr)
}

func (m *DnfManager) isDnfLocked(err error) bool {
	errStr := err.Error()
	return strings.Contains(errStr, "Another app is currently holding the lock") ||
		strings.Contains(errStr, "Could not get lock") ||
		strings.Contains(errStr, "dnf lock")
}

// PacmanManager manages packages via pacman.
type PacmanManager struct {
	UseSudo bool
}

func NewPacmanManager() *PacmanManager {
	return &PacmanManager{}
}

func (m *PacmanManager) Name() string {
	return "pacman"
}

func (m *PacmanManager) IsInstalled() bool {
	_, err := lookPath("pacman")
	return err == nil
}

func (m *PacmanManager) Install(pkgs []string) error {
	if len(pkgs) == 0 {
		return nil
	}
	bin := "pacman"
	args := append([]string{"-S", "--noconfirm"}, pkgs...)
	if m.UseSudo {
		args = append([]string{bin}, args...)
		bin = "sudo"
	}

	return m.runWithRetry(bin, args...)
}

func (m *PacmanManager) runWithRetry(bin string, args ...string) error {
	const maxRetries = 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		cmd := execCommand(bin, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		err := cmd.Run()

		if err == nil {
			return nil
		}

		lastErr = err

		// Check for pacman lock
		if m.isPacmanLocked(err) {
			if attempt < maxRetries-1 {
				waitTime := time.Duration(attempt+1) * 5 * time.Second
				fmt.Fprintf(os.Stderr, "pacman lock detected, waiting %v before retry (%d/%d)...\n", waitTime, attempt+1, maxRetries)
				time.Sleep(waitTime)
				continue
			}
			return fmt.Errorf("pacman lock could not be acquired after %d attempts: %w", maxRetries, err)
		}

		// For other errors, retry with exponential backoff
		if attempt < maxRetries-1 {
			waitTime := time.Duration(1<<attempt) * 2 * time.Second
			fmt.Fprintf(os.Stderr, "Install failed, retrying in %v (%d/%d): %v\n", waitTime, attempt+1, maxRetries, err)
			time.Sleep(waitTime)
			continue
		}
	}

	return fmt.Errorf("failed after %d attempts: %w", maxRetries, lastErr)
}

func (m *PacmanManager) isPacmanLocked(err error) bool {
	errStr := err.Error()
	return strings.Contains(errStr, "unable to lock database") ||
		strings.Contains(errStr, "pacman lock") ||
		strings.Contains(errStr, "/var/lib/pacman/db.lck")
}

// MiseManager manages tools via mise.
type MiseManager struct{}

func NewMiseManager() *MiseManager {
	return &MiseManager{}
}

func (m *MiseManager) Name() string {
	return "mise"
}

func (m *MiseManager) IsInstalled() bool {
	_, err := lookPath("mise")
	return err == nil
}

func (m *MiseManager) Install(pkgs []string) error {
	if len(pkgs) == 0 {
		return nil
	}
	args := append([]string{"install"}, pkgs...)
	cmd := execCommand("mise", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// UvManager manages python dependencies via uv pip.
type UvManager struct{}

func NewUvManager() *UvManager {
	return &UvManager{}
}

func (m *UvManager) Name() string {
	return "uv"
}

func (m *UvManager) IsInstalled() bool {
	_, err := lookPath("uv")
	return err == nil
}

func (m *UvManager) Install(pkgs []string) error {
	if len(pkgs) == 0 {
		return nil
	}
	args := append([]string{"pip", "install", "--system"}, pkgs...)
	cmd := execCommand("uv", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// CargoManager manages rust binaries/crates via cargo.
type CargoManager struct{}

func NewCargoManager() *CargoManager {
	return &CargoManager{}
}

func (m *CargoManager) Name() string {
	return "cargo"
}

func (m *CargoManager) IsInstalled() bool {
	_, err := lookPath("cargo")
	return err == nil
}

func (m *CargoManager) Install(pkgs []string) error {
	if len(pkgs) == 0 {
		return nil
	}
	args := append([]string{"install"}, pkgs...)
	cmd := execCommand("cargo", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// VcpkgManager manages C/C++ libraries via vcpkg.
type VcpkgManager struct{}

func NewVcpkgManager() *VcpkgManager {
	return &VcpkgManager{}
}

func (m *VcpkgManager) Name() string {
	return "vcpkg"
}

func (m *VcpkgManager) IsInstalled() bool {
	_, err := lookPath("vcpkg")
	return err == nil
}

func (m *VcpkgManager) Install(pkgs []string) error {
	if len(pkgs) == 0 {
		return nil
	}
	args := append([]string{"install"}, pkgs...)
	cmd := execCommand("vcpkg", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
