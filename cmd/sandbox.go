package cmd

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var getProcessInfoFunc = getProcessInfo

// CheckAncestorForGhPt traverses parent processes upwards up to PID 1.
// Returns true if any ancestor process name is "gh-pt" (or "gh-pt.exe").
func CheckAncestorForGhPt() (bool, error) {
	return checkAncestorWithGetter(getProcessInfoFunc)
}

func checkAncestorWithGetter(getter func(int) (string, int, error)) (bool, error) {
	currentPID := os.Getppid() // Start from parent process
	visited := make(map[int]bool)
	maxDepth := 64

	for depth := 0; depth < maxDepth; depth++ {
		if currentPID <= 1 || visited[currentPID] {
			break
		}
		visited[currentPID] = true

		name, ppid, err := getter(currentPID)
		if err != nil {
			// Process terminated or inaccessible (e.g. permission boundary)
			break
		}

		cleanName := strings.TrimSuffix(filepath.Base(name), ".exe")
		if strings.EqualFold(cleanName, "gh-pt") {
			return true, nil
		}

		if ppid <= 1 || ppid == currentPID {
			break
		}
		currentPID = ppid
	}

	return false, nil
}

// IsHelperInvocation checks if the CLI arguments represent a helper subcommand.
func IsHelperInvocation(args []string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		return arg == "helper"
	}
	return false
}

// getProcessInfo retrieves the process name and PPID for a given PID.
// Linux implementation reads /proc/[pid]/stat and /proc/[pid]/comm.
func getProcessInfo(pid int) (string, int, error) {
	statPath := "/proc/" + strconv.Itoa(pid) + "/stat"
	data, err := os.ReadFile(statPath)
	if err != nil {
		return "", 0, err
	}

	statStr := string(data)
	lastParen := strings.LastIndexByte(statStr, ')')
	if lastParen == -1 || lastParen+2 >= len(statStr) {
		return "", 0, os.ErrInvalid
	}

	// The process comm is between the first and last parenthesis
	firstParen := strings.IndexByte(statStr, '(')
	comm := ""
	if firstParen != -1 && firstParen < lastParen {
		comm = statStr[firstParen+1 : lastParen]
	}

	// After ') ', fields are: state (field 3), ppid (field 4), etc.
	fields := strings.Fields(statStr[lastParen+1:])
	if len(fields) < 2 {
		return "", 0, os.ErrInvalid
	}

	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return "", 0, err
	}

	// Prefer reading /proc/[pid]/comm if available for full executable name
	if commBytes, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm"); err == nil {
		clean := strings.TrimSpace(string(commBytes))
		if clean != "" {
			comm = clean
		}
	}

	return comm, ppid, nil
}
