package compile

import (
	"os/exec"
	"strings"
	"testing"
)

// TestContainerSecurityHardening verifies that container execution has proper security controls
func TestContainerSecurityHardening(t *testing.T) {
	// Skip if no container runtime available
	runtimeName, err := DetectContainerRuntime()
	if err != nil {
		t.Skip("No container runtime available")
	}

	// Use fully qualified image name for podman compatibility
	image := "docker.io/library/alpine:latest"

	// Test 1: Network isolation
	t.Run("NetworkIsolation", func(t *testing.T) {
		cmd := exec.Command(runtimeName, "run", "--rm", "--network=none", image, "sh", "-c", "ping -c 1 8.8.8.8 2>&1 || echo NETWORK_BLOCKED")
		output, _ := cmd.CombinedOutput()
		if !strings.Contains(string(output), "NETWORK_BLOCKED") {
			t.Errorf("Network should be disabled in container. Output: %s", string(output))
		}
	})

	// Test 2: Capability dropping
	t.Run("CapabilityDropping", func(t *testing.T) {
		cmd := exec.Command(runtimeName, "run", "--rm", "--cap-drop=ALL", "--cap-add=DAC_OVERRIDE", image, "sh", "-c", "cat /proc/1/status | grep CapEff")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Failed to check capabilities: %v, output: %s", err, string(output))
		}
		// DAC_OVERRIDE is 0x2, should be the only capability
		if !strings.Contains(string(output), "0000000000000002") {
			t.Errorf("Expected only DAC_OVERRIDE capability, got: %s", string(output))
		}
	})

	// Test 3: Read-only filesystem
	t.Run("ReadOnlyFilesystem", func(t *testing.T) {
		cmd := exec.Command(runtimeName, "run", "--rm", "--read-only", image, "sh", "-c", "touch /test 2>&1 || echo READONLY_BLOCKED")
		output, _ := cmd.CombinedOutput()
		if !strings.Contains(string(output), "READONLY_BLOCKED") {
			t.Errorf("Root filesystem should be read-only. Output: %s", string(output))
		}
	})

	// Test 4: Non-root user
	t.Run("NonRootUser", func(t *testing.T) {
		cmd := exec.Command(runtimeName, "run", "--rm", "--user=1000:1000", image, "id", "-u")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Failed to check user: %v, output: %s", err, string(output))
		}
		if !strings.Contains(string(output), "1000") {
			t.Errorf("Expected user 1000, got: %s", string(output))
		}
	})

	// Test 5: No-new-privileges
	t.Run("NoNewPrivileges", func(t *testing.T) {
		cmd := exec.Command(runtimeName, "run", "--rm", "--security-opt=no-new-privileges", image, "cat", "/proc/1/status")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Failed to check no-new-privileges: %v, output: %s", err, string(output))
		}
		if !strings.Contains(string(output), "NoNewPrivs:\t1") {
			t.Errorf("no-new-privileges should be set. Output: %s", string(output))
		}
	})

	// Test 6: Tmpfs for /tmp
	t.Run("TmpfsForTmp", func(t *testing.T) {
		cmd := exec.Command(runtimeName, "run", "--rm", "--read-only", "--tmpfs=/tmp:size=100M", image, "mount")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Failed to check mounts: %v, output: %s", err, string(output))
		}
		if !strings.Contains(string(output), "tmpfs on /tmp") {
			t.Errorf("/tmp should be mounted as tmpfs. Output: %s", string(output))
		}
	})
}

// TestBuildContainerCommand verifies that the command builder includes security flags
func TestBuildContainerCommand(t *testing.T) {
	cfg := &Config{
		RepoDir:          "/test/repo",
		InstallDirTarget: "/test/install",
	}

	runtimeName := "docker"
	image := "alpine:latest"

	args := BuildContainerCommand(cfg, runtimeName, image)

	// Check for security flags
	expectedFlags := []string{
		"--network=none",
		"--cap-drop=ALL",
		"--cap-add=DAC_OVERRIDE",
		"--security-opt=no-new-privileges",
		"--read-only",
		"--user=1000:1000",
	}

	for _, flag := range expectedFlags {
		found := false
		for _, arg := range args {
			if strings.Contains(arg, flag) || arg == flag {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected security flag %s not found in command", flag)
		}
	}

	// Check for read-only source mount
	foundSourceRO := false
	for i, arg := range args {
		if arg == "-v" && i+1 < len(args) && strings.Contains(args[i+1], "/build:ro") {
			foundSourceRO = true
			break
		}
	}
	if !foundSourceRO {
		t.Error("Source directory should be mounted read-only")
	}
}
