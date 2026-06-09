package clienv

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildEnvAddsBinaryDirBeforeLaunchdStylePATH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH separator and Unix-style common dirs differ on Windows")
	}

	binDir := t.TempDir()
	binary := filepath.Join(binDir, "codex")
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("AGENTRE_CLIENV_PARENT", "from-parent")

	env := BuildEnv(map[string]string{"AGENTRE_CLIENV_PARENT": "from-extra"}, binary)

	path, ok := Lookup(env, "PATH")
	require.True(t, ok)
	parts := filepath.SplitList(path)
	require.NotEmpty(t, parts)
	assert.Equal(t, binDir, parts[0])
	assert.Contains(t, parts, "/usr/bin")
	assert.Contains(t, parts, "/bin")

	got, ok := Lookup(env, "AGENTRE_CLIENV_PARENT")
	require.True(t, ok)
	assert.Equal(t, "from-extra", got)
}

func TestSearchPathFromIncludesCommonMacCLIPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-style common dirs do not apply to Windows")
	}

	path := SearchPathFrom("/usr/bin:/bin", "")
	parts := filepath.SplitList(path)

	assert.Contains(t, parts, "/opt/homebrew/bin")
	assert.Contains(t, parts, "/usr/local/bin")
	assert.Contains(t, parts, "/usr/bin")
	assert.Contains(t, parts, "/bin")
}

func TestResolveBinaryForEnvUsesAugmentedPATH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("exec-bit lookup differs on Windows")
	}

	binDir := t.TempDir()
	binary := filepath.Join(binDir, "agentre-cli-test")
	require.NoError(t, os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o755))

	env := BuildEnv(map[string]string{"PATH": binDir}, "agentre-cli-test")
	got, ok := ResolveBinaryForEnv("agentre-cli-test", env)

	require.True(t, ok)
	assert.Equal(t, binary, got)
}

func TestResolveBinaryInPathUsesWindowsPATHEXT(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PATHEXT lookup only applies on Windows")
	}

	binDir := t.TempDir()
	want := filepath.Join(binDir, "claude.exe")
	require.NoError(t, os.WriteFile(want, []byte{}, 0o644))
	t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")

	got, ok := ResolveBinaryInPath("claude", binDir)

	require.True(t, ok)
	assert.Equal(t, want, got)
}

func TestResolveBinaryInPathPrefersWindowsPATHEXTOverExtensionlessShim(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PATHEXT lookup only applies on Windows")
	}

	binDir := t.TempDir()
	posixShim := filepath.Join(binDir, "pi")
	want := filepath.Join(binDir, "pi.cmd")
	require.NoError(t, os.WriteFile(posixShim, []byte("#!/bin/sh\nexit 0\n"), 0o644))
	require.NoError(t, os.WriteFile(want, []byte("@echo off\r\nexit /b 0\r\n"), 0o644))
	t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")

	got, ok := ResolveBinaryInPath("pi", binDir)

	require.True(t, ok)
	assert.Equal(t, want, got)
}
