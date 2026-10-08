package safety

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsVitalOrProtected(t *testing.T) {
	// Empty & Root
	assert.True(t, IsVitalOrProtected(""))
	assert.True(t, IsVitalOrProtected("/"))
	assert.True(t, IsVitalOrProtected("."))

	// Standard system paths
	assert.True(t, IsVitalOrProtected("/usr"))
	assert.True(t, IsVitalOrProtected("/usr/bin"))
	assert.True(t, IsVitalOrProtected("/usr/local"))
	assert.True(t, IsVitalOrProtected("/usr/local/bin"))
	assert.True(t, IsVitalOrProtected("/etc"))
	assert.True(t, IsVitalOrProtected("/var"))
	assert.True(t, IsVitalOrProtected("/opt"))
	assert.True(t, IsVitalOrProtected("/bin"))
	assert.True(t, IsVitalOrProtected("/sbin"))

	// User vital paths
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		assert.True(t, IsVitalOrProtected(home))
		assert.True(t, IsVitalOrProtected(filepath.Join(home, ".local")))
		assert.True(t, IsVitalOrProtected(filepath.Join(home, ".local", "bin")))
		assert.True(t, IsVitalOrProtected(filepath.Join(home, "bin")))
		assert.True(t, IsVitalOrProtected(filepath.Join(home, "Desktop")))
		assert.True(t, IsVitalOrProtected(filepath.Join(home, "src")))
		assert.True(t, IsVitalOrProtected(filepath.Join(home, "builds")))
	}

	// Deep subpath in temp dir should NOT be protected
	tmpDir := t.TempDir()
	safeChild := filepath.Join(tmpDir, "gh-pt-test", "subdir")
	assert.False(t, IsVitalOrProtected(safeChild))
}

func TestIsAncestorOfVital(t *testing.T) {
	assert.True(t, IsAncestorOfVital("/"))
	assert.True(t, IsAncestorOfVital("/usr"))
	assert.True(t, IsAncestorOfVital("/usr/local"))

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		assert.True(t, IsAncestorOfVital(home))
	}

	// Deep temp directory is NOT an ancestor of vital
	tmpDir := t.TempDir()
	child := filepath.Join(tmpDir, "some-random-package")
	assert.False(t, IsAncestorOfVital(child))
}

func TestAssertSafeToRemoveAll_BlocksVitals(t *testing.T) {
	testCases := []string{
		"",
		"/",
		".",
		"/usr",
		"/usr/bin",
		"/usr/local",
		"/usr/local/bin",
		"/etc",
		"/var",
		"/bin",
		"/home",
		"/opt",
	}

	for _, tc := range testCases {
		t.Run("Blocks_"+tc, func(t *testing.T) {
			err := AssertSafeToRemoveAll(tc)
			assert.Error(t, err)
		})
	}
}

func TestAssertSafeToRemoveAll_BlocksHomeAndVitalUserDirs(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	userVitals := []string{
		home,
		filepath.Join(home, "bin"),
		filepath.Join(home, ".local"),
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, ".config"),
		filepath.Join(home, ".cache"),
		filepath.Join(home, "builds"),
		filepath.Join(home, "src"),
		filepath.Join(home, "Desktop"),
	}

	for _, v := range userVitals {
		t.Run("Blocks_"+v, func(t *testing.T) {
			err := AssertSafeToRemoveAll(v)
			assert.Error(t, err)
		})
	}
}

func TestAssertSafeToRemoveAll_BlocksSymlinkToVital(t *testing.T) {
	tmpDir := t.TempDir()
	symlinkPath := filepath.Join(tmpDir, "fake_bin")

	// Symlink pointing to /usr/local/bin
	require.NoError(t, os.Symlink("/usr/local/bin", symlinkPath))

	err := AssertSafeToRemoveAll(symlinkPath)
	assert.Error(t, err)
}

func TestAssertSafeToRemoveAll_AllowsSafeSandbox(t *testing.T) {
	tmpDir := t.TempDir()
	sandboxDir := filepath.Join(tmpDir, "gh-pt-pkg", "owner", "repo")
	require.NoError(t, os.MkdirAll(sandboxDir, 0755))

	err := AssertSafeToRemoveAll(sandboxDir)
	assert.NoError(t, err)

	// Can safely RemoveAll
	err = RemoveAll(sandboxDir)
	assert.NoError(t, err)
	assert.NoDirExists(t, sandboxDir)
}

func TestAssertSafeToRemoveAll_AllowsXdgDataGhpt(t *testing.T) {
	tmpDir := t.TempDir()
	origXdg := xdg.DataHome
	xdg.DataHome = tmpDir
	defer func() { xdg.DataHome = origXdg }()

	pkgDir := filepath.Join(tmpDir, "gh-pt", "packages", "owner", "app")
	require.NoError(t, os.MkdirAll(pkgDir, 0755))

	err := AssertSafeToRemoveAll(pkgDir)
	assert.NoError(t, err)

	// But xdg.DataHome itself is protected!
	err = AssertSafeToRemoveAll(tmpDir)
	assert.Error(t, err)
}

func TestSafeDeletePath(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0755))

	validFile := filepath.Join(binDir, "my-app")
	require.NoError(t, os.WriteFile(validFile, []byte("bin"), 0755))

	// Valid deletion
	target, err := SafeDeletePath(binDir, "my-app")
	require.NoError(t, err)
	assert.Equal(t, validFile, target)

	// Traversal attacks
	_, err = SafeDeletePath(binDir, "../etc/passwd")
	assert.Error(t, err)

	_, err = SafeDeletePath(binDir, "..")
	assert.Error(t, err)

	_, err = SafeDeletePath(binDir, "/usr/bin/python")
	assert.Error(t, err)

	_, err = SafeDeletePath(binDir, "")
	assert.Error(t, err)
}

func TestPruneEmptyParentDirs_StopsAtProtectedDir(t *testing.T) {
	tmpDir := t.TempDir()
	origXdg := xdg.DataHome
	xdg.DataHome = tmpDir
	defer func() { xdg.DataHome = origXdg }()

	// Create nested structure: tmpDir/gh-pt/sidecars/repo/nested/empty
	nestedDir := filepath.Join(tmpDir, "gh-pt", "sidecars", "repo", "nested", "empty")
	require.NoError(t, os.MkdirAll(nestedDir, 0755))

	// Prune starting from empty
	PruneEmptyParentDirs(nestedDir, []string{tmpDir})

	// tmpDir and tmpDir/gh-pt must NOT be deleted
	assert.DirExists(t, tmpDir)
}

func TestAssertSafeToModify_BlocksOutsideScopedTempWhenEnforced(t *testing.T) {
	tempSandbox := t.TempDir()
	outsideDir := t.TempDir()

	insideFile := filepath.Join(tempSandbox, "allowed.txt")
	outsideFile := filepath.Join(outsideDir, "blocked.txt")

	SetScopedTempDir(tempSandbox)
	EnforceScopedTempOnly(true)
	defer func() {
		SetScopedTempDir("")
		EnforceScopedTempOnly(false)
	}()

	// File inside scoped temp dir is allowed
	assert.NoError(t, AssertSafeToModify(insideFile))
	assert.NoError(t, WriteFile(insideFile, []byte("ok"), 0644))
	assert.FileExists(t, insideFile)

	// File outside scoped temp dir is blocked
	err := AssertSafeToModify(outsideFile)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrModificationOutsideScope)

	// Operations using WriteFile, Create, Remove are blocked outside scope
	assert.ErrorIs(t, WriteFile(outsideFile, []byte("bad"), 0644), ErrModificationOutsideScope)
	_, err = Create(outsideFile)
	assert.ErrorIs(t, err, ErrModificationOutsideScope)
	assert.ErrorIs(t, Remove(outsideFile), ErrModificationOutsideScope)
	assert.ErrorIs(t, RemoveAll(outsideDir), ErrModificationOutsideScope)
}

func TestAssertSafeToModify_BlocksOutsideScopedTempInDryRun(t *testing.T) {
	tempSandbox := t.TempDir()
	systemOrTargetDir := t.TempDir()

	insideFile := filepath.Join(tempSandbox, "stage.bin")
	outsideFile := filepath.Join(systemOrTargetDir, "app_binary")

	SetScopedTempDir(tempSandbox)
	SetDryRun(true)
	defer func() {
		SetScopedTempDir("")
		SetDryRun(false)
	}()

	// In dry run, file inside scoped temp dir can still be staged/inspected
	assert.NoError(t, AssertSafeToModify(insideFile))
	assert.NoError(t, WriteFile(insideFile, []byte("staged payload"), 0644))

	// Modifying anything outside scoped temp is blocked
	err := AssertSafeToModify(outsideFile)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrModificationOutsideScope)

	assert.ErrorIs(t, WriteFile(outsideFile, []byte("fail"), 0644), ErrModificationOutsideScope)
	assert.ErrorIs(t, Remove(outsideFile), ErrModificationOutsideScope)
	assert.ErrorIs(t, RemoveAll(systemOrTargetDir), ErrModificationOutsideScope)
}

func TestWithScopedTemp_EnforcesScopedBoundary(t *testing.T) {
	sandbox := t.TempDir()
	outside := t.TempDir()

	insidePath := filepath.Join(sandbox, "data.json")
	outsidePath := filepath.Join(outside, "secret.json")

	err := WithScopedTemp(sandbox, func() error {
		// Inside sandbox: writes succeed
		if err := WriteFile(insidePath, []byte("{}"), 0644); err != nil {
			return err
		}
		// Outside sandbox: blocked
		if err := WriteFile(outsidePath, []byte("{}"), 0644); err == nil {
			t.Fatal("expected write to outsidePath to fail")
		}
		return nil
	})
	require.NoError(t, err)

	assert.FileExists(t, insidePath)
	assert.NoFileExists(t, outsidePath)
}

func TestSafeMkdirTemp(t *testing.T) {
	baseDir := t.TempDir()

	t.Run("NormalSafeCreation", func(t *testing.T) {
		tempDir, err := SafeMkdirTemp(baseDir, "safe-test-*")
		require.NoError(t, err)
		defer os.RemoveAll(tempDir)

		assert.DirExists(t, tempDir)
		assert.True(t, HasGhptManagedMarker(tempDir))
	})

	t.Run("ParentIsSymlinkBlocked", func(t *testing.T) {
		targetDir := filepath.Join(baseDir, "real_target")
		require.NoError(t, os.MkdirAll(targetDir, 0755))

		symlinkParent := filepath.Join(baseDir, "symlink_parent")
		require.NoError(t, os.Symlink(targetDir, symlinkParent))

		_, err := SafeMkdirTemp(symlinkParent, "hijack-*")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "is a symlink")
	})
}
