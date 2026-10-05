package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

func main() {
	parentPID := os.Getenv("GHPT_AI_PARENT_PID")
	if parentPID == "" {
		fmt.Fprintln(os.Stderr, "ERROR: GHPT_AI_PARENT_PID not set")
		os.Exit(1)
	}

	pid, err := strconv.Atoi(parentPID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: invalid PID: %s\n", parentPID)
		os.Exit(1)
	}

	proc, err := os.FindProcess(pid)
	if err != nil || proc.Signal(syscall.Signal(0)) != nil {
		fmt.Fprintln(os.Stderr, "Parent process not found, forwarding to gh-pt...")
		forwardToRealGhpt(os.Args[1:])
		return
	}

	if len(os.Args) < 2 || os.Args[1] != "helper" {
		fmt.Fprintln(os.Stderr, "Only 'ghpt helper' is permitted within this AI session.")
		fmt.Fprintln(os.Stderr, "If you need to install a dependency, use:")
		fmt.Fprintln(os.Stderr, "  ghpt helper --append-manifest \"manager=package@version\"")
		os.Exit(1)
	}

	forwardToRealGhpt(os.Args[1:])
}

func forwardToRealGhpt(args []string) {
	realPath := os.Getenv("GHPT_REAL_PATH")
	if realPath == "" {
		// Try to find the real ghpt by looking for it in standard locations
		// Skip the current executable (which is the wrapper)
		currentExe, err := os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, "ERROR: cannot determine current executable path")
			os.Exit(1)
		}

		// Look for ghpt in PATH, but skip if it's the wrapper itself
		pathDirs := filepath.SplitList(os.Getenv("PATH"))
		for _, dir := range pathDirs {
			candidate := filepath.Join(dir, "ghpt")
			if candidate == currentExe {
				continue // Skip ourselves
			}
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				realPath = candidate
				break
			}
		}

		if realPath == "" {
			fmt.Fprintln(os.Stderr, "ERROR: cannot find real gh-pt binary")
			os.Exit(1)
		}
	}

	cmd := exec.Command(realPath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		os.Exit(1)
	}
}
