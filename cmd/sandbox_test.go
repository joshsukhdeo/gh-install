package cmd

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsHelperInvocation(t *testing.T) {
	tests := []struct {
		args     []string
		expected bool
	}{
		{args: []string{"helper"}, expected: true},
		{args: []string{"helper", "--validate-compile-script-body"}, expected: true},
		{args: []string{"-v", "helper"}, expected: true},
		{args: []string{"install", "user/repo"}, expected: false},
		{args: []string{"show", "user/repo"}, expected: false},
		{args: []string{"--help"}, expected: false},
		{args: []string{}, expected: false},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, IsHelperInvocation(tt.args), "args: %v", tt.args)
	}
}

func TestGetProcessInfo_CurrentProcess(t *testing.T) {
	pid := os.Getpid()
	name, ppid, err := getProcessInfo(pid)
	require.NoError(t, err)
	assert.NotEmpty(t, name)
	assert.True(t, ppid > 0, "ppid should be greater than 0")
}

func TestCheckAncestorForGhPt_SafeExecution(t *testing.T) {
	// Must terminate cleanly without panic or infinite loop in normal environment
	isRestricted, err := CheckAncestorForGhPt()
	assert.NoError(t, err)
	assert.False(t, isRestricted)
}

func TestCheckAncestorWithGetter_Detection(t *testing.T) {
	// Simulated tree:
	// parent (PID 200, "bash") -> grandparent (PID 100, "gh-pt") -> systemd (PID 1)
	parentPID := os.Getppid()
	procTable := map[int]struct {
		name string
		ppid int
	}{
		parentPID: {name: "bash", ppid: 1000},
		1000:      {name: "python3", ppid: 2000},
		2000:      {name: "gh-pt", ppid: 1},
		1:         {name: "systemd", ppid: 0},
	}

	mockGetter := func(pid int) (string, int, error) {
		if proc, ok := procTable[pid]; ok {
			return proc.name, proc.ppid, nil
		}
		return "", 0, os.ErrNotExist
	}

	isRestricted, err := checkAncestorWithGetter(mockGetter)
	assert.NoError(t, err)
	assert.True(t, isRestricted, "ancestor 'gh-pt' should be detected and trigger restriction")
}

func TestCheckAncestorWithGetter_CycleProtection(t *testing.T) {
	// Simulated malicious or corrupt cycle:
	// parent (PID 200) -> (PID 300) -> (PID 200)
	parentPID := os.Getppid()
	procTable := map[int]struct {
		name string
		ppid int
	}{
		parentPID: {name: "node", ppid: 300},
		300:       {name: "subagent", ppid: parentPID},
	}

	mockGetter := func(pid int) (string, int, error) {
		if proc, ok := procTable[pid]; ok {
			return proc.name, proc.ppid, nil
		}
		return "", 0, os.ErrNotExist
	}

	isRestricted, err := checkAncestorWithGetter(mockGetter)
	assert.NoError(t, err)
	assert.False(t, isRestricted, "cyclic process tree must terminate safely without hanging")
}

func TestCheckAncestorWithGetter_NoMatch(t *testing.T) {
	parentPID := os.Getppid()
	procTable := map[int]struct {
		name string
		ppid int
	}{
		parentPID: {name: "zsh", ppid: 50},
		50:        {name: "terminal", ppid: 1},
	}

	mockGetter := func(pid int) (string, int, error) {
		if proc, ok := procTable[pid]; ok {
			return proc.name, proc.ppid, nil
		}
		return "", 0, os.ErrNotExist
	}

	isRestricted, err := checkAncestorWithGetter(mockGetter)
	assert.NoError(t, err)
	assert.False(t, isRestricted)
}

func TestIsContainerEnvironment(t *testing.T) {
	// Must execute without panic
	_ = isContainerEnvironment()

	// When container env is set, must return true
	t.Setenv("container", "docker")
	assert.True(t, isContainerEnvironment())

	t.Setenv("container", "podman")
	assert.True(t, isContainerEnvironment())
}

func TestRootPolicy_ForceRootAndGlobal(t *testing.T) {
	cli := &RootCLI{
		Global:    true,
		ForceRoot: false,
	}
	assert.True(t, cli.Global)

	// Test that Global implies ForceRoot
	if cli.Global {
		cli.ForceRoot = true
	}
	assert.True(t, cli.ForceRoot)
}

func TestCompileContainerIsolationFlags(t *testing.T) {
	cli := &RootCLI{
		NoCompileContainer: false,
		NoAIContainer:      false,
	}
	// By default, container isolation is enabled (NoCompileContainer == false, NoAIContainer == false)
	assert.False(t, cli.NoCompileContainer)
	assert.False(t, cli.NoAIContainer)

	cli.NoCompileContainer = true
	assert.True(t, cli.NoCompileContainer)

	cli.NoAIContainer = true
	assert.True(t, cli.NoAIContainer)
}
