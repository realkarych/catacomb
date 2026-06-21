package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readHooks(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	var s map[string]any
	require.NoError(t, json.Unmarshal(b, &s))
	hooks, _ := s["hooks"].(map[string]any)
	return hooks
}

func TestInstallHooksFreshWritesAllEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude", "settings.json")
	require.NoError(t, installHooks(path, "/run/d.json", "/usr/bin/catacomb", false))
	hooks := readHooks(t, path)
	assert.Len(t, hooks, len(hookEvents))
	for _, ev := range hookEvents {
		assert.Contains(t, hooks, ev)
	}
}

func TestInstallHooksPreservesOtherKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"model":"opus","hooks":{"PreToolUse":[{"matcher":"","hooks":[{"type":"command","command":"other"}]}]}}`), 0o644))
	require.NoError(t, installHooks(path, "/run/d.json", "/usr/bin/catacomb", false))
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	var s map[string]any
	require.NoError(t, json.Unmarshal(b, &s))
	assert.Equal(t, "opus", s["model"])
	pre := s["hooks"].(map[string]any)["PreToolUse"].([]any)
	assert.Len(t, pre, 2)
}

func TestInstallHooksIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	require.NoError(t, installHooks(path, "/run/d.json", "/usr/bin/catacomb", false))
	require.NoError(t, installHooks(path, "/run/d.json", "/usr/bin/catacomb", false))
	pre := readHooks(t, path)["PreToolUse"].([]any)
	assert.Len(t, pre, 1)
}

func TestInstallHooksReinstallDifferentExeNoDuplicate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	require.NoError(t, installHooks(path, "/run/d.json", "/old/path/catacomb", false))
	require.NoError(t, installHooks(path, "/run/d.json", "/new/path/catacomb", false))
	pre := readHooks(t, path)["PreToolUse"].([]any)
	require.Len(t, pre, 1)
	cmd := pre[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"].(string)
	assert.Contains(t, cmd, "/new/path/catacomb")
}

func TestInstallHooksUninstall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"hooks":{"PreToolUse":[{"matcher":"","hooks":[{"type":"command","command":"other"}]}]}}`), 0o644))
	require.NoError(t, installHooks(path, "/run/d.json", "/usr/bin/catacomb", false))
	require.NoError(t, installHooks(path, "/run/d.json", "/usr/bin/catacomb", true))
	pre := readHooks(t, path)["PreToolUse"].([]any)
	require.Len(t, pre, 1)
	entry := pre[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)
	assert.Equal(t, "other", entry["command"])
}

func TestInstallHooksMalformedSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	require.NoError(t, os.WriteFile(path, []byte("{not json}"), 0o644))
	require.Error(t, installHooks(path, "/run/d.json", "/usr/bin/catacomb", false))
}

func TestInstallHooksReadError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "afile")
	require.NoError(t, os.WriteFile(dir, []byte("x"), 0o600))
	require.Error(t, installHooks(filepath.Join(dir, "settings.json"), "/run/d.json", "/usr/bin/catacomb", false))
}

func TestSettingsPathProjectDefault(t *testing.T) {
	p, err := settingsPath(false, false)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(".claude", "settings.json"), p)
}

func TestSettingsPathGlobal(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	p, perr := settingsPath(false, true)
	require.NoError(t, perr)
	assert.Equal(t, filepath.Join(home, ".claude", "settings.json"), p)
}

func TestSettingsPathBothFlags(t *testing.T) {
	_, err := settingsPath(true, true)
	require.Error(t, err)
}

func TestInstallHooksCommandWiring(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	root := newRootCmd()
	root.SetArgs([]string{"install-hooks"})
	require.NoError(t, root.Execute())
	assert.FileExists(t, filepath.Join(dir, ".claude", "settings.json"))
}

func TestInstallHooksCommandBothFlags(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"install-hooks", "--project", "--global"})
	require.Error(t, root.Execute())
}

func TestInstallHooksCommandExecutableError(t *testing.T) {
	old := osExecutable
	t.Cleanup(func() { osExecutable = old })
	osExecutable = func() (string, error) { return "", errors.New("exe") }
	t.Chdir(t.TempDir())
	root := newRootCmd()
	root.SetArgs([]string{"install-hooks"})
	require.Error(t, root.Execute())
}

func TestSettingsPathGlobalHomeError(t *testing.T) {
	old := osUserHomeDir
	t.Cleanup(func() { osUserHomeDir = old })
	osUserHomeDir = func() (string, error) { return "", errors.New("no home") }
	_, err := settingsPath(false, true)
	require.Error(t, err)
}

func TestInstallHooksUninstallEmptiesHooks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	require.NoError(t, installHooks(path, "/run/d.json", "/usr/bin/catacomb", false))
	require.NoError(t, installHooks(path, "/run/d.json", "/usr/bin/catacomb", true))
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	var s map[string]any
	require.NoError(t, json.Unmarshal(b, &s))
	_, hasHooks := s["hooks"]
	assert.False(t, hasHooks)
}

func TestReadSettingsNullJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	require.NoError(t, os.WriteFile(path, []byte("null"), 0o644))
	require.NoError(t, installHooks(path, "/run/d.json", "/usr/bin/catacomb", false))
	hooks := readHooks(t, path)
	assert.Len(t, hooks, len(hookEvents))
}

func TestWriteSettingsMkdirError(t *testing.T) {
	base := t.TempDir()
	f := filepath.Join(base, "notadir")
	require.NoError(t, os.WriteFile(f, []byte("x"), 0o644))
	path := filepath.Join(f, "sub", "settings.json")
	err := writeSettings(path, map[string]any{})
	require.Error(t, err)
}

func TestWriteSettingsWriteError(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "claude")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	settingsFile := filepath.Join(dir, "settings.json")
	require.NoError(t, os.MkdirAll(settingsFile, 0o755))
	err := writeSettings(settingsFile, map[string]any{})
	require.Error(t, err)
}

func TestWriteSettingsMarshalError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	ch := make(chan int)
	err := writeSettings(path, map[string]any{"bad": ch})
	require.Error(t, err)
}

func TestEntryHasMarkerNonMapEntry(t *testing.T) {
	assert.False(t, entryHasMarker("not-a-map", "catacomb"))
}

func TestEntryHasMarkerNonMapHook(t *testing.T) {
	entry := map[string]any{
		"hooks": []any{"not-a-map"},
	}
	assert.False(t, entryHasMarker(entry, "catacomb"))
}
