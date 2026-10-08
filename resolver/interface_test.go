package resolver

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Helper to mock execCommand for testing CLI executions without running actual commands
func mockExecCommand(captured *[]string, shouldFail bool) func(name string, arg ...string) *exec.Cmd {
	return func(name string, arg ...string) *exec.Cmd {
		fullCmd := append([]string{name}, arg...)
		*captured = append(*captured, strings.Join(fullCmd, " "))

		// We return a command that executes the test binary helper or true/false
		if shouldFail {
			return exec.Command("false")
		}
		return exec.Command("true")
	}
}

func TestPackageManagerInterfaces(t *testing.T) {
	managers := []struct {
		mgr      PackageManager
		expected string
	}{
		{NewAptManager(), "apt"},
		{NewDnfManager(), "dnf"},
		{NewPacmanManager(), "pacman"},
		{NewMiseManager(), "mise"},
		{NewUvManager(), "uv"},
		{NewCargoManager(), "cargo"},
		{NewVcpkgManager(), "vcpkg"},
	}

	for _, m := range managers {
		if m.mgr.Name() != m.expected {
			t.Errorf("expected manager name %q, got %q", m.expected, m.mgr.Name())
		}
	}
}

func TestIsInstalled(t *testing.T) {
	origLookPath := lookPath
	defer func() { lookPath = origLookPath }()

	tests := []struct {
		name          string
		mgr           PackageManager
		mockFound     map[string]bool
		expectInstall bool
	}{
		{
			name:          "apt installed via apt-get",
			mgr:           NewAptManager(),
			mockFound:     map[string]bool{"apt-get": true},
			expectInstall: true,
		},
		{
			name:          "apt installed via apt",
			mgr:           NewAptManager(),
			mockFound:     map[string]bool{"apt": true},
			expectInstall: true,
		},
		{
			name:          "apt not installed",
			mgr:           NewAptManager(),
			mockFound:     map[string]bool{},
			expectInstall: false,
		},
		{
			name:          "dnf installed",
			mgr:           NewDnfManager(),
			mockFound:     map[string]bool{"dnf": true},
			expectInstall: true,
		},
		{
			name:          "pacman installed",
			mgr:           NewPacmanManager(),
			mockFound:     map[string]bool{"pacman": true},
			expectInstall: true,
		},
		{
			name:          "mise installed",
			mgr:           NewMiseManager(),
			mockFound:     map[string]bool{"mise": true},
			expectInstall: true,
		},
		{
			name:          "uv installed",
			mgr:           NewUvManager(),
			mockFound:     map[string]bool{"uv": true},
			expectInstall: true,
		},
		{
			name:          "cargo installed",
			mgr:           NewCargoManager(),
			mockFound:     map[string]bool{"cargo": true},
			expectInstall: true,
		},
		{
			name:          "vcpkg installed",
			mgr:           NewVcpkgManager(),
			mockFound:     map[string]bool{"vcpkg": true},
			expectInstall: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookPath = func(file string) (string, error) {
				if tt.mockFound[file] {
					return "/bin/" + file, nil
				}
				return "", exec.ErrNotFound
			}

			if got := tt.mgr.IsInstalled(); got != tt.expectInstall {
				t.Errorf("%s: IsInstalled() = %v, expected %v", tt.name, got, tt.expectInstall)
			}
		})
	}
}

func TestInstallCommands(t *testing.T) {
	origExecCommand := execCommand
	origLookPath := lookPath
	defer func() {
		execCommand = origExecCommand
		lookPath = origLookPath
	}()

	lookPath = func(file string) (string, error) {
		return "/bin/" + file, nil
	}

	tests := []struct {
		name        string
		mgr         PackageManager
		pkgs        []string
		expectedCmd string
	}{
		{
			name:        "apt install packages",
			mgr:         &AptManager{UseSudo: false},
			pkgs:        []string{"pkgA", "pkgB"},
			expectedCmd: "apt-get install -y pkgA pkgB",
		},
		{
			name:        "apt with sudo",
			mgr:         &AptManager{UseSudo: true},
			pkgs:        []string{"pkgA"},
			expectedCmd: "sudo apt-get install -y pkgA",
		},
		{
			name:        "dnf install packages",
			mgr:         &DnfManager{UseSudo: false},
			pkgs:        []string{"pkgA", "pkgB"},
			expectedCmd: "dnf install -y pkgA pkgB",
		},
		{
			name:        "dnf with sudo",
			mgr:         &DnfManager{UseSudo: true},
			pkgs:        []string{"pkgA"},
			expectedCmd: "sudo dnf install -y pkgA",
		},
		{
			name:        "pacman install packages",
			mgr:         &PacmanManager{UseSudo: false},
			pkgs:        []string{"pkgA", "pkgB"},
			expectedCmd: "pacman -S --noconfirm pkgA pkgB",
		},
		{
			name:        "pacman with sudo",
			mgr:         &PacmanManager{UseSudo: true},
			pkgs:        []string{"pkgA"},
			expectedCmd: "sudo pacman -S --noconfirm pkgA",
		},
		{
			name:        "mise install tools",
			mgr:         NewMiseManager(),
			pkgs:        []string{"node@20", "python@3.12"},
			expectedCmd: "mise install node@20 python@3.12",
		},
		{
			name:        "uv install packages",
			mgr:         NewUvManager(),
			pkgs:        []string{"requests", "numpy"},
			expectedCmd: "uv pip install --system requests numpy",
		},
		{
			name:        "cargo install crates",
			mgr:         NewCargoManager(),
			pkgs:        []string{"ripgrep", "bat"},
			expectedCmd: "cargo install ripgrep bat",
		},
		{
			name:        "vcpkg install libraries",
			mgr:         NewVcpkgManager(),
			pkgs:        []string{"fmt", "spdlog"},
			expectedCmd: "vcpkg install fmt spdlog",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured []string
			execCommand = mockExecCommand(&captured, false)

			err := tt.mgr.Install(tt.pkgs)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(captured) != 1 {
				t.Fatalf("expected 1 command executed, got %d", len(captured))
			}

			if captured[0] != tt.expectedCmd {
				t.Errorf("command mismatch:\nexpected: %q\ngot:      %q", tt.expectedCmd, captured[0])
			}
		})
	}
}

func TestInstallEmptyPackages(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	var captured []string
	execCommand = mockExecCommand(&captured, false)

	managers := []PackageManager{
		NewAptManager(),
		NewDnfManager(),
		NewPacmanManager(),
		NewMiseManager(),
		NewUvManager(),
		NewCargoManager(),
		NewVcpkgManager(),
	}

	for _, m := range managers {
		err := m.Install([]string{})
		if err != nil {
			t.Errorf("%s returned error on empty pkgs: %v", m.Name(), err)
		}
	}

	if len(captured) != 0 {
		t.Errorf("expected no commands executed for empty packages, got %d: %v", len(captured), captured)
	}
}

func TestInstallErrorPropagation(t *testing.T) {
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()

	var captured []string
	execCommand = mockExecCommand(&captured, true) // shouldFail = true

	mgr := NewAptManager()
	err := mgr.Install([]string{"broken-pkg"})
	if err == nil {
		t.Fatal("expected error when command execution fails, got nil")
	}
}

func TestGetNativeManager(t *testing.T) {
	origLookPath := lookPath
	origOsReleasePath := osReleasePath
	defer func() {
		lookPath = origLookPath
		osReleasePath = origOsReleasePath
	}()

	tempDir := t.TempDir()
	emptyReleaseFile := filepath.Join(tempDir, "os-release-empty")
	if err := os.WriteFile(emptyReleaseFile, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	fedoraReleaseFile := filepath.Join(tempDir, "os-release-fedora")
	if err := os.WriteFile(fedoraReleaseFile, []byte("ID=fedora\nID_LIKE=rhel"), 0644); err != nil {
		t.Fatal(err)
	}

	archReleaseFile := filepath.Join(tempDir, "os-release-arch")
	if err := os.WriteFile(archReleaseFile, []byte("ID=arch\nID_LIKE=archlinux"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Run("detects apt when available", func(t *testing.T) {
		osReleasePath = emptyReleaseFile
		lookPath = func(file string) (string, error) {
			if file == "apt-get" || file == "apt" {
				return "/usr/bin/" + file, nil
			}
			return "", exec.ErrNotFound
		}

		mgr, err := GetNativeManager()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mgr.Name() != "apt" {
			t.Errorf("expected manager name 'apt', got %q", mgr.Name())
		}
	})

	t.Run("detects dnf on fedora", func(t *testing.T) {
		osReleasePath = fedoraReleaseFile
		lookPath = func(file string) (string, error) {
			if file == "dnf" || file == "apt-get" {
				return "/usr/bin/" + file, nil
			}
			return "", exec.ErrNotFound
		}

		mgr, err := GetNativeManager()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mgr.Name() != "dnf" {
			t.Errorf("expected manager name 'dnf', got %q", mgr.Name())
		}
	})

	t.Run("detects pacman on arch", func(t *testing.T) {
		osReleasePath = archReleaseFile
		lookPath = func(file string) (string, error) {
			if file == "pacman" {
				return "/usr/bin/pacman", nil
			}
			return "", exec.ErrNotFound
		}

		mgr, err := GetNativeManager()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mgr.Name() != "pacman" {
			t.Errorf("expected manager name 'pacman', got %q", mgr.Name())
		}
	})

	t.Run("returns error when none available", func(t *testing.T) {
		osReleasePath = emptyReleaseFile
		lookPath = func(file string) (string, error) {
			return "", exec.ErrNotFound
		}

		mgr, err := GetNativeManager()
		if err == nil {
			t.Fatal("expected ErrNoNativeManager, got nil error")
		}
		if !errors.Is(err, ErrNoNativeManager) {
			t.Errorf("expected ErrNoNativeManager, got: %v", err)
		}
		if mgr != nil {
			t.Errorf("expected nil manager on error, got %v", mgr)
		}
	})
}

func TestGetManager(t *testing.T) {
	tests := []struct {
		name        string
		target      string
		expectedErr bool
		expectedMgr string
	}{
		{name: "apt", target: "apt", expectedErr: false, expectedMgr: "apt"},
		{name: "dnf", target: "dnf", expectedErr: false, expectedMgr: "dnf"},
		{name: "pacman", target: "pacman", expectedErr: false, expectedMgr: "pacman"},
		{name: "mise", target: "mise", expectedErr: false, expectedMgr: "mise"},
		{name: "uv", target: "uv", expectedErr: false, expectedMgr: "uv"},
		{name: "cargo", target: "cargo", expectedErr: false, expectedMgr: "cargo"},
		{name: "vcpkg", target: "vcpkg", expectedErr: false, expectedMgr: "vcpkg"},
		{name: "unknown", target: "foobar", expectedErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, err := GetManager(tt.target)
			if tt.expectedErr {
				if err == nil {
					t.Errorf("expected error for %s, got nil", tt.target)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error for %s: %v", tt.target, err)
				}
				if mgr == nil || mgr.Name() != tt.expectedMgr {
					t.Errorf("expected manager %s, got %v", tt.expectedMgr, mgr)
				}
			}
		})
	}
}

func TestManagerTypes(t *testing.T) {
	// Verify concrete types
	var _ PackageManager = (*AptManager)(nil)
	var _ PackageManager = (*DnfManager)(nil)
	var _ PackageManager = (*PacmanManager)(nil)
	var _ PackageManager = (*MiseManager)(nil)
	var _ PackageManager = (*UvManager)(nil)
	var _ PackageManager = (*CargoManager)(nil)
	var _ PackageManager = (*VcpkgManager)(nil)

	if reflect.TypeOf((*PackageManager)(nil)).Elem().NumMethod() != 3 {
		t.Errorf("PackageManager interface should have 3 methods")
	}
}
