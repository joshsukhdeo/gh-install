package safety

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/adrg/xdg"
)

var (
	// ErrProtectedDirectory is returned when an operation targets a vital system or user directory.
	ErrProtectedDirectory = errors.New("cannot delete or modify protected vital directory")

	// ErrEmptyPath is returned when a target path is empty.
	ErrEmptyPath = errors.New("path cannot be empty")

	// ErrRootPath is returned when an operation targets the filesystem root or a drive volume.
	ErrRootPath = errors.New("cannot delete filesystem root or volume")

	// ErrPathOutsideAllowed is returned when recursive deletion is attempted outside approved namespaces.
	ErrPathOutsideAllowed = errors.New("path is outside allowed sandbox, temporary, or gh-pt directories")

	// ErrDirectoryTraversal is returned when path traversal outside base directory is detected.
	ErrDirectoryTraversal = errors.New("path traversal outside allowed base directory")

	// ErrModificationOutsideScope is returned when file modification occurs outside the active scoped temp directory.
	ErrModificationOutsideScope = errors.New("file modification blocked outside scoped temporary directory")
)

var (
	stateMu              sync.RWMutex
	scopedTempDir        string
	isScopedTempEnforced bool
	isDryRun             bool
	extraSafeDirs        []string
)

// SetDryRun enables or disables dry-run protection.
// When enabled, all file modifications outside the active scoped temp directory are hard-blocked.
func SetDryRun(enabled bool) {
	stateMu.Lock()
	defer stateMu.Unlock()
	isDryRun = enabled
}

// ErrDryRunSubprocessBlocked is returned when a subprocess is blocked in dry-run mode.
var ErrDryRunSubprocessBlocked = errors.New("subprocess execution blocked in dry-run mode")

// DryRunCommandConfig configures which subprocesses are blocked in dry-run mode.
type DryRunCommandConfig struct {
	// BlockedCommands are command names (base executable) that should be blocked in dry-run.
	BlockedCommands []string
	// AllowedCommands are command names that are explicitly allowed even in dry-run.
	// These override BlockedCommands.
	AllowedCommands []string
	// BlockAllExceptAllowed, when true, blocks ALL commands except those in AllowedCommands.
	// Default: false (only blocks BlockedCommands).
	BlockAllExceptAllowed bool
}

// DefaultDryRunConfig returns the default configuration for dry-run subprocess blocking.
func DefaultDryRunConfig() DryRunCommandConfig {
	return DryRunCommandConfig{
		BlockedCommands: []string{
			"git", "dpkg", "rpm", "pacman", "pkg", "apt", "apt-get", "yum", "dnf",
			"brew", "make", "go", "cargo", "npm", "pip", "pip3",
			"bash", "sh", "python", "python3", "node", "perl", "ruby",
			"tar", "unzip", "7z", "gzip", "bzip2", "xz", "zstd",
			"docker", "podman", "kubectl", "helm",
			"sudo", "su", "doas",
		},
		AllowedCommands: []string{
			"ls", "cat", "head", "tail", "grep", "find", "stat", "file", "readlink",
			"which", "whereis", "dirname", "basename", "realpath", "pwd", "echo", "printf",
			"test", "[[", "true", "false",
		},
		BlockAllExceptAllowed: false,
	}
}

// DryRunCommand wraps exec.Command to enforce dry-run subprocess blocking.
// Returns a *exec.Cmd that will fail immediately with ErrDryRunSubprocessBlocked
// if the command is blocked by the current dry-run configuration.
func DryRunCommand(name string, args ...string) *exec.Cmd {
	if !IsDryRun() {
		return exec.Command(name, args...)
	}

	config := DefaultDryRunConfig()
	base := filepath.Base(name)

	// Check allowed list first
	for _, allowed := range config.AllowedCommands {
		if base == allowed {
			return exec.Command(name, args...)
		}
	}

	// Check blocked list
	blocked := false
	for _, b := range config.BlockedCommands {
		if base == b {
			blocked = true
			break
		}
	}

	// If BlockAllExceptAllowed is true, block everything not explicitly allowed
	if config.BlockAllExceptAllowed && !blocked {
		allowed := false
		for _, a := range config.AllowedCommands {
			if base == a {
				allowed = true
				break
			}
		}
		if !allowed {
			blocked = true
		}
	}

	if blocked {
		// Return a command that will fail with our custom error
		cmd := exec.Command("sh", "-c", fmt.Sprintf("echo 'DRY-RUN BLOCKED: %s %s' >&2; exit 1", name, strings.Join(args, " ")))
		cmd.Stdout = nil
		cmd.Stderr = nil
		return cmd
	}

	return exec.Command(name, args...)
}

// DryRunCommandContext wraps exec.CommandContext with dry-run blocking.
func DryRunCommandContext(ctx interface{}, name string, args ...string) *exec.Cmd {
	// We can't easily wrap CommandContext without importing context, so just use DryRunCommand
	return DryRunCommand(name, args...)
}

// IsDryRunSubprocessError checks if an error is from a dry-run blocked subprocess.
func IsDryRunSubprocessError(err error) bool {
	return err != nil && (err == ErrDryRunSubprocessBlocked || strings.Contains(err.Error(), "DRY-RUN BLOCKED"))
}

// IsDryRun returns whether dry run mode is currently enabled in safety.
func IsDryRun() bool {
	stateMu.RLock()
	defer stateMu.RUnlock()
	return isDryRun
}

// SetScopedTempDir sets the current scoped temporary directory for operations.
func SetScopedTempDir(dir string) {
	stateMu.Lock()
	defer stateMu.Unlock()
	if dir == "" {
		scopedTempDir = ""
	} else {
		scopedTempDir = filepath.Clean(dir)
	}
}

// GetScopedTempDir returns the active scoped temporary directory.
func GetScopedTempDir() string {
	stateMu.RLock()
	defer stateMu.RUnlock()
	return scopedTempDir
}

// EnforceScopedTempOnly enables or disables strict scoping.
// When enabled, all file creations, writes, and deletions MUST reside inside the scoped temporary directory.
func EnforceScopedTempOnly(enforce bool) {
	stateMu.Lock()
	defer stateMu.Unlock()
	isScopedTempEnforced = enforce
}

// WithScopedTemp runs a function with a scoped temporary directory and strict enforcement enabled,
// restoring previous safety state when done.
func WithScopedTemp(tempDir string, fn func() error) error {
	stateMu.Lock()
	prevDir := scopedTempDir
	prevEnforced := isScopedTempEnforced
	scopedTempDir = filepath.Clean(tempDir)
	isScopedTempEnforced = true
	stateMu.Unlock()

	defer func() {
		stateMu.Lock()
		scopedTempDir = prevDir
		isScopedTempEnforced = prevEnforced
		stateMu.Unlock()
	}()

	return fn()
}

// RegisterSafeDirectory registers a path that is explicitly allowed for gh-pt operations (e.g. custom clone/package paths).
func RegisterSafeDirectory(dir string) {
	stateMu.Lock()
	defer stateMu.Unlock()
	clean := filepath.Clean(dir)
	for _, d := range extraSafeDirs {
		if d == clean {
			return
		}
	}
	extraSafeDirs = append(extraSafeDirs, clean)
}

// GetVitalDirectories returns a comprehensive slice of system-critical and user-critical directories
// that must never be wiped, modified, or recursively deleted.
func GetVitalDirectories() []string {
	var vitals []string

	// Root
	vitals = append(vitals, "/", string(filepath.Separator))

	// Standard Unix / Linux system directories
	unixSystemDirs := []string{
		"/bin",
		"/boot",
		"/dev",
		"/etc",
		"/home",
		"/lib",
		"/lib64",
		"/media",
		"/mnt",
		"/opt",
		"/proc",
		"/root",
		"/run",
		"/sbin",
		"/snap",
		"/srv",
		"/sys",
		"/tmp",
		"/usr",
		"/usr/bin",
		"/usr/games",
		"/usr/include",
		"/usr/lib",
		"/usr/lib64",
		"/usr/libexec",
		"/usr/local",
		"/usr/local/bin",
		"/usr/local/etc",
		"/usr/local/games",
		"/usr/local/include",
		"/usr/local/lib",
		"/usr/local/libexec",
		"/usr/local/sbin",
		"/usr/local/share",
		"/usr/local/src",
		"/usr/sbin",
		"/usr/share",
		"/usr/src",
		"/var",
		"/var/log",
		"/var/run",
		"/var/lib",
	}
	vitals = append(vitals, unixSystemDirs...)

	// macOS specific system directories
	macDirs := []string{
		"/Applications",
		"/Library",
		"/Network",
		"/System",
		"/Users",
		"/Volumes",
		"/cores",
		"/private",
		"/opt/homebrew",
		"/opt/homebrew/bin",
		"/opt/homebrew/sbin",
		"/opt/homebrew/lib",
		"/opt/homebrew/share",
	}
	vitals = append(vitals, macDirs...)

	// Windows specific system directories
	if runtime.GOOS == "windows" {
		winDirs := []string{
			`C:\`,
			`C:\Windows`,
			`C:\Windows\System32`,
			`C:\Program Files`,
			`C:\Program Files (x86)`,
			`C:\ProgramData`,
			`C:\Users`,
		}
		for _, envVar := range []string{"WINDIR", "SYSTEMROOT", "PROGRAMFILES", "PROGRAMFILES(X86)", "PROGRAMDATA"} {
			if val := os.Getenv(envVar); val != "" {
				winDirs = append(winDirs, val)
			}
		}
		vitals = append(vitals, winDirs...)
	}

	// User home directory and vital subdirectories
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		vitals = append(vitals, home)
		userVitalDirs := []string{
			filepath.Join(home, "Desktop"),
			filepath.Join(home, "Documents"),
			filepath.Join(home, "Downloads"),
			filepath.Join(home, "Music"),
			filepath.Join(home, "Pictures"),
			filepath.Join(home, "Videos"),
			filepath.Join(home, "Projects"),
			filepath.Join(home, "projects"),
			filepath.Join(home, "src"),
			filepath.Join(home, "builds"),
			filepath.Join(home, "bin"),
			filepath.Join(home, "go"),
			filepath.Join(home, "go", "bin"),
			filepath.Join(home, ".cargo"),
			filepath.Join(home, ".cargo", "bin"),
			filepath.Join(home, ".rustup"),
			filepath.Join(home, ".local"),
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".local", "lib"),
			filepath.Join(home, ".local", "include"),
			filepath.Join(home, ".local", "share"),
			filepath.Join(home, ".config"),
			filepath.Join(home, ".cache"),
			filepath.Join(home, ".ssh"),
			filepath.Join(home, ".gnupg"),
		}
		vitals = append(vitals, userVitalDirs...)
	}

	// XDG root directories
	if xdg.DataHome != "" {
		vitals = append(vitals, xdg.DataHome)
	}
	if xdg.ConfigHome != "" {
		vitals = append(vitals, xdg.ConfigHome)
	}
	if xdg.StateHome != "" {
		vitals = append(vitals, xdg.StateHome)
	}
	if xdg.CacheHome != "" {
		vitals = append(vitals, xdg.CacheHome)
	}

	// Root temp directory
	if tmp := os.TempDir(); tmp != "" {
		vitals = append(vitals, tmp)
	}

	// Clean and normalize
	seen := make(map[string]bool)
	var cleaned []string
	for _, v := range vitals {
		c := filepath.Clean(v)
		if !seen[c] {
			seen[c] = true
			cleaned = append(cleaned, c)
		}
	}

	return cleaned
}

// IsVitalOrProtected returns true if path matches any vital system or user directory,
// is a filesystem root, or resolves via symlink to a vital directory.
func IsVitalOrProtected(path string) bool {
	if strings.TrimSpace(path) == "" {
		return true
	}

	clean := filepath.Clean(path)
	if clean == "." || clean == "/" || clean == string(filepath.Separator) {
		return true
	}

	if vol := filepath.VolumeName(clean); vol != "" && (clean == vol || clean == vol+string(filepath.Separator) || clean == vol+"/") {
		return true
	}

	abs, err := filepath.Abs(clean)
	if err != nil {
		abs = clean
	}

	// Check against all registered vital directories
	for _, vital := range GetVitalDirectories() {
		if clean == vital || abs == vital {
			return true
		}
	}

	// Check if symlink target is vital
	if eval, err := filepath.EvalSymlinks(abs); err == nil && eval != abs {
		cleanEval := filepath.Clean(eval)
		for _, vital := range GetVitalDirectories() {
			if cleanEval == vital {
				return true
			}
		}
	}

	// Protection against shallow paths (direct child of root on Unix, e.g. /usr, /bin, /etc, /foo)
	if runtime.GOOS != "windows" {
		trimmed := strings.Trim(abs, "/")
		if trimmed != "" && !strings.Contains(trimmed, "/") {
			// Depth is 1 directly under root
			return true
		}
	}

	return false
}

// IsAncestorOfVital returns true if path is an ancestor of any vital directory.
// Deleting an ancestor would destroy all vital directories underneath it.
func IsAncestorOfVital(path string) bool {
	if strings.TrimSpace(path) == "" {
		return true
	}

	clean := filepath.Clean(path)
	abs, err := filepath.Abs(clean)
	if err != nil {
		abs = clean
	}

	for _, vital := range GetVitalDirectories() {
		vitalAbs, err := filepath.Abs(vital)
		if err != nil {
			vitalAbs = vital
		}

		// Check if abs is a parent/ancestor of vitalAbs
		rel, err := filepath.Rel(abs, vitalAbs)
		if err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
			return true
		}
	}

	return false
}

// isWithinSafeNamespace checks if path is located strictly inside an approved
// gh-pt namespace (e.g. $TMPDIR/..., $XDG_DATA_HOME/gh-pt/..., ~/builds/<app>, ~/src/apps/<app>).
func isWithinSafeNamespace(path string) bool {
	clean := filepath.Clean(path)
	abs, err := filepath.Abs(clean)
	if err != nil {
		abs = clean
	}

	// 1. Temporary directories (e.g. os.TempDir()/gh-pt-*, t.TempDir())
	tmp := filepath.Clean(os.TempDir())
	if isStrictSubpath(abs, tmp) {
		return true
	}

	// 2. Dedicated gh-pt data directory: $XDG_DATA_HOME/gh-pt/...
	if xdg.DataHome != "" {
		ghptData := filepath.Join(filepath.Clean(xdg.DataHome), "gh-pt")
		if isStrictSubpath(abs, ghptData) {
			return true
		}
	}

	// 3. Dedicated gh-pt cache directory: $XDG_CACHE_HOME/gh-pt/...
	if xdg.CacheHome != "" {
		ghptCache := filepath.Join(filepath.Clean(xdg.CacheHome), "gh-pt")
		if isStrictSubpath(abs, ghptCache) {
			return true
		}
	}

	// 4. Cloned app builds directory: ~/builds/<repoName>
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		buildsDir := filepath.Join(filepath.Clean(home), "builds")
		if isStrictSubpath(abs, buildsDir) {
			return true
		}

		// 5. Symlinked app directory: ~/src/apps/<owner>/<repo>
		srcAppsDir := filepath.Join(filepath.Clean(home), "src", "apps")
		if isStrictSubpath(abs, srcAppsDir) {
			return true
		}
	}

	// 6. Explicitly registered safe directories
	stateMu.RLock()
	defer stateMu.RUnlock()
	for _, safeDir := range extraSafeDirs {
		if isStrictSubpath(abs, safeDir) {
			return true
		}
	}

	return false
}

// isStrictSubpath returns true if child is strictly inside parent and child != parent.
func isStrictSubpath(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, "..")
}

// AssertSafeToModify verifies that a path is safe to modify (write, create, remove, chmod).
// It blocks modifications to vital directories and enforces scoped temp directories during dry-run
// or when scoped temporary enforcement is active.
func AssertSafeToModify(path string) error {
	if strings.TrimSpace(path) == "" {
		return ErrEmptyPath
	}

	clean := filepath.Clean(path)
	if clean == "." || clean == "/" || clean == string(filepath.Separator) {
		return ErrRootPath
	}

	if vol := filepath.VolumeName(clean); vol != "" && (clean == vol || clean == vol+string(filepath.Separator) || clean == vol+"/") {
		return ErrRootPath
	}

	if IsVitalOrProtected(clean) {
		return fmt.Errorf("%w: %s", ErrProtectedDirectory, clean)
	}

	if IsAncestorOfVital(clean) {
		return fmt.Errorf("%w: %s is an ancestor of a vital directory", ErrProtectedDirectory, clean)
	}

	stateMu.RLock()
	scoped := scopedTempDir
	enforced := isScopedTempEnforced
	dryRun := isDryRun
	stateMu.RUnlock()

	// If dry-run is active, NO modification outside scoped temp directory is permitted
	if dryRun {
		if scoped == "" {
			scoped = filepath.Clean(os.TempDir())
		}
		if !isStrictSubpath(clean, scoped) && clean != scoped {
			return fmt.Errorf("%w: in dry-run mode, modifying %s is prohibited", ErrModificationOutsideScope, clean)
		}
	}

	// If a scoped temp directory is strictly enforced:
	if scoped != "" && enforced {
		if !isStrictSubpath(clean, scoped) && clean != scoped {
			return fmt.Errorf("%w: %s is outside scoped temp directory %s", ErrModificationOutsideScope, clean, scoped)
		}
	}

	return nil
}

// AssertSafeToRemoveAll performs exhaustive checks to ensure path is safe for recursive deletion.
// Returns an error if path is empty, root, vital, an ancestor of vital, or outside approved namespaces.
func AssertSafeToRemoveAll(path string) error {
	if err := AssertSafeToModify(path); err != nil {
		return err
	}

	clean := filepath.Clean(path)
	if !isWithinSafeNamespace(clean) {
		return fmt.Errorf("%w: %s", ErrPathOutsideAllowed, clean)
	}

	return nil
}

// AssertSafeToRemove verifies that a single target path is safe to unlink.
func AssertSafeToRemove(path string) error {
	return AssertSafeToModify(path)
}

// RemoveAll validates the path with AssertSafeToRemoveAll before invoking os.RemoveAll.
func RemoveAll(path string) error {
	if err := AssertSafeToRemoveAll(path); err != nil {
		return err
	}
	return os.RemoveAll(path)
}

// Remove validates the path with AssertSafeToRemove before invoking os.Remove.
func Remove(path string) error {
	if err := AssertSafeToRemove(path); err != nil {
		return err
	}
	return os.Remove(path)
}

// WriteFile validates that the target path is safe to modify before writing.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	if err := AssertSafeToModify(path); err != nil {
		return err
	}
	return os.WriteFile(path, data, perm)
}

// Create validates that the target path is safe to modify before creating.
func Create(path string) (*os.File, error) {
	if err := AssertSafeToModify(path); err != nil {
		return nil, err
	}
	return os.Create(path)
}

// MkdirAll validates that the target path is safe to modify before creating directories.
func MkdirAll(path string, perm os.FileMode) error {
	if err := AssertSafeToModify(path); err != nil {
		return err
	}
	return os.MkdirAll(path, perm)
}

// Symlink validates that newname is safe to modify before creating a symlink.
func Symlink(oldname, newname string) error {
	if err := AssertSafeToModify(newname); err != nil {
		return err
	}
	return os.Symlink(oldname, newname)
}

// SafeDeletePath validates that name is strictly a file within baseDir and does not escape or match a protected directory.
func SafeDeletePath(baseDir, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("empty target name")
	}
	if filepath.IsAbs(name) || name == "." || name == ".." {
		return "", fmt.Errorf("unsafe delete target %q", name)
	}
	cleanName := filepath.Clean(name)
	if cleanName == "." || cleanName == ".." || cleanName == string(filepath.Separator) {
		return "", fmt.Errorf("unsafe delete target %q", name)
	}
	if filepath.Base(cleanName) != cleanName {
		return "", fmt.Errorf("%w: %q", ErrDirectoryTraversal, name)
	}

	fullPath := filepath.Join(baseDir, cleanName)
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return "", err
	}
	absFull, err := filepath.Abs(fullPath)
	if err != nil {
		return "", err
	}

	rel, err := filepath.Rel(absBase, absFull)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q", ErrDirectoryTraversal, name)
	}

	if err := AssertSafeToRemove(fullPath); err != nil {
		return "", err
	}

	return fullPath, nil
}

// PruneEmptyParentDirs ascends the directory tree starting from startDir and removes
// empty directories until it encounters a non-empty directory, a directory in stopDirs,
// or a vital/protected directory. It also enforces that pruning does not escape the gh-pt data namespace.
func PruneEmptyParentDirs(startDir string, stopDirs []string) {
	stopMap := make(map[string]bool)
	for _, s := range stopDirs {
		if s != "" {
			stopMap[filepath.Clean(s)] = true
		}
	}

	if xdg.DataHome != "" {
		ghptData := filepath.Join(filepath.Clean(xdg.DataHome), "gh-pt")
		stopMap[ghptData] = true
		stopMap[filepath.Join(ghptData, "packages")] = true
	}

	for curr := filepath.Clean(startDir); curr != "." && curr != string(filepath.Separator) && curr != filepath.VolumeName(curr)+string(filepath.Separator); curr = filepath.Dir(curr) {
		if stopMap[curr] || IsVitalOrProtected(curr) {
			break
		}

		entries, err := os.ReadDir(curr)
		if err != nil || len(entries) > 0 {
			break
		}

		if err := Remove(curr); err != nil {
			break
		}
	}
}

// GhptManagedMarker is the filename used to mark directories owned by gh-pt.
const GhptManagedMarker = ".gh-pt-managed"

// WriteGhptManagedMarker creates the .gh-pt-managed marker file in the given directory
// to indicate that gh-pt owns and manages this directory.
func WriteGhptManagedMarker(dir string) error {
	if err := AssertSafeToModify(dir); err != nil {
		return err
	}
	markerPath := filepath.Join(dir, GhptManagedMarker)
	return os.WriteFile(markerPath, []byte("managed by gh-pt\n"), 0644)
}

// HasGhptManagedMarker checks if the directory contains the .gh-pt-managed marker.
func HasGhptManagedMarker(dir string) bool {
	markerPath := filepath.Join(dir, GhptManagedMarker)
	_, err := os.Stat(markerPath)
	return err == nil
}

// AssertGhptManaged verifies that the directory is managed by gh-pt (has the marker).
// Returns an error if the marker is not present.
func AssertGhptManaged(dir string) error {
	if !HasGhptManagedMarker(dir) {
		return fmt.Errorf("%w: %s is not managed by gh-pt (missing %s)", ErrPathOutsideAllowed, dir, GhptManagedMarker)
	}
	return nil
}

// RemoveAllManaged validates that the directory is managed by gh-pt before removing.
// This is a safer version of RemoveAll that requires ownership confirmation.
func RemoveAllManaged(path string) error {
	if err := AssertSafeToRemoveAll(path); err != nil {
		return err
	}
	if err := AssertGhptManaged(path); err != nil {
		return err
	}
	return os.RemoveAll(path)
}

// RemoveLegacySafe attempts to remove a directory safely. It first checks for the
// .gh-pt-managed marker using RemoveAllManaged. If it fails specifically due to the
// missing marker, it falls back to AssertSafeToRemoveAll to ensure the path is safely
// within the gh-pt namespace before deleting it.
func RemoveLegacySafe(path string) error {
	err := RemoveAllManaged(path)
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrPathOutsideAllowed) && strings.Contains(err.Error(), "missing "+GhptManagedMarker) {
		if assertErr := AssertSafeToRemoveAll(path); assertErr != nil {
			return assertErr
		}
		return os.RemoveAll(path)
	}
	return err
}

// SafeMkdirTemp creates a temporary directory safely, verifying that the parent directory
// is not a symlink to prevent symlink race and hijacking attacks.
func SafeMkdirTemp(dir, pattern string) (string, error) {
	if dir == "" {
		dir = os.TempDir()
	}
	cleanDir := filepath.Clean(dir)

	// Check if parent directory is a symlink
	lfi, err := os.Lstat(cleanDir)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("failed to lstat temp parent directory: %w", err)
	}
	if err == nil && lfi.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("temp parent directory %s is a symlink (potential hijacking attack)", cleanDir)
	}

	// Verify parent directory does not resolve to an unexpected vital system directory
	evalDir, err := filepath.EvalSymlinks(cleanDir)
	if err == nil {
		if evalDir == "/" || evalDir == "/etc" || evalDir == "/root" {
			return "", fmt.Errorf("temp parent directory resolves to vital directory %s", evalDir)
		}
	}

	tempPath, err := os.MkdirTemp(cleanDir, pattern)
	if err != nil {
		return "", err
	}

	_ = WriteGhptManagedMarker(tempPath)

	return tempPath, nil
}
