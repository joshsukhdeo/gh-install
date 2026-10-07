package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/joshsukhdeo/gh-pt/params"
	"github.com/joshsukhdeo/gh-pt/safety"
	"github.com/joshsukhdeo/gh-pt/state"
	"github.com/pterm/pterm"
)

var hookExecCommand = exec.Command

func init() {
	params.HookRunner = func(h *params.Hook) error {
		r := &RootCLI{
			ExecContext: params.ExecContext{
				Repository:     h.Repository,
				HookEvent:      h.Event,
				HookScriptPath: h.ScriptPath,
			},
			Hook: *h,
		}
		return r.handleHook()
	}
}

// handleHook saves a user-defined hook script for a repository into state.json.
func (r *RootCLI) handleHook() error {
	event := r.Hook.Event
	if event == "" {
		event = r.HookEvent
	}
	repo := r.Hook.Repository
	if repo == "" {
		repo = r.Repository
	}
	scriptPath := r.Hook.ScriptPath
	if scriptPath == "" {
		scriptPath = r.HookScriptPath
	}

	if event != "post-install" && event != "pre-uninstall" {
		return fmt.Errorf("invalid hook event %q: must be 'post-install' or 'pre-uninstall'", event)
	}

	if repo == "" {
		return fmt.Errorf("repository is required")
	}

	if scriptPath == "" {
		return fmt.Errorf("script path is required")
	}

	data, err := os.ReadFile(scriptPath)
	if err != nil {
		return fmt.Errorf("failed to read hook script: %w", err)
	}

	st, err := state.LoadState()
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	if st.Apps == nil {
		st.Apps = make(map[string]*state.InstalledApp)
	}

	app, exists := st.Apps[repo]
	if !exists || app == nil {
		app = &state.InstalledApp{
			Repository: repo,
		}
		st.Apps[repo] = app
	}

	if app.Hooks == nil {
		app.Hooks = make(map[string]string)
	}

	app.Hooks[event] = string(data)

	if err := st.Save(); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	pterm.Success.Printf("Configured %s hook for %s\n", event, repo)
	return nil
}

// executeHookScript writes a hook script to a temp file, marks it executable, executes it, and cleans it up.
func executeHookScript(event, repo, script string) error {
	if safety.IsDryRun() {
		pterm.Info.Printf("[dry-run] Would execute %s hook for %s\n", event, repo)
		return nil
	}

	tmpFile, err := os.CreateTemp("", fmt.Sprintf("gh-pt-hook-%s-*.sh", event))
	if err != nil {
		return fmt.Errorf("failed to create temp file for %s hook: %w", event, err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmpFile.WriteString(script); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write %s hook to temp file: %w", event, err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp hook file: %w", err)
	}

	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("failed to make %s hook executable: %w", event, err)
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = hookExecCommand("powershell", "-ExecutionPolicy", "Bypass", "-File", tmpPath)
	} else {
		trimmed := strings.TrimSpace(script)
		if !strings.HasPrefix(trimmed, "#!") {
			if _, err := exec.LookPath("bash"); err == nil {
				cmd = hookExecCommand("bash", tmpPath)
			} else {
				cmd = hookExecCommand("sh", tmpPath)
			}
		} else {
			cmd = hookExecCommand(tmpPath)
		}
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = os.Environ()

	pterm.Info.Printf("Executing %s hook for %s...\n", event, repo)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s hook failed for %s: %w", event, repo, err)
	}
	pterm.Success.Printf("Successfully executed %s hook for %s\n", event, repo)
	return nil
}
