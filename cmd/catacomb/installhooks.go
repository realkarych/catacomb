package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/daemon"
)

var hookEvents = []string{
	"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse",
	"SubagentStop", "Stop", "SessionEnd", "PreCompact", "Notification",
}

var (
	osExecutable  = os.Executable
	osUserHomeDir = os.UserHomeDir
)

func newInstallHooksCmd() *cobra.Command {
	var project, global, uninstall bool
	cmd := &cobra.Command{
		Use:   "install-hooks",
		Short: "Wire the catacomb hook forwarder into Claude Code settings.json",
		Long: `Wire the catacomb hook forwarder into Claude Code settings.json.

--project (default) writes ./.claude/settings.json and observes only this
directory. --global writes ~/.claude/settings.json and observes every project.`,
		Example: `  # current project only
  catacomb install-hooks

  # every project
  catacomb install-hooks --global`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := settingsPath(project, global)
			if err != nil {
				return err
			}
			exe, err := osExecutable()
			if err != nil {
				return fmt.Errorf("install-hooks executable: %w", err)
			}
			return installHooks(path, daemon.DiscoveryPath(), exe, uninstall)
		},
	}
	cmd.Flags().BoolVar(&project, "project", false, "write .claude/settings.json in the current directory (default)")
	cmd.Flags().BoolVar(&global, "global", false, "write ~/.claude/settings.json")
	cmd.Flags().BoolVar(&uninstall, "uninstall", false, "remove catacomb hook entries")
	return cmd
}

func settingsPath(project, global bool) (string, error) {
	if project && global {
		return "", fmt.Errorf("install-hooks: choose at most one of --project / --global")
	}
	if global {
		home, err := osUserHomeDir()
		if err != nil {
			return "", fmt.Errorf("install-hooks home: %w", err)
		}
		return filepath.Join(home, ".claude", "settings.json"), nil
	}
	return filepath.Join(".claude", "settings.json"), nil
}

func installHooks(path, discoveryPath, exe string, uninstall bool) error {
	settings, err := readSettings(path)
	if err != nil {
		return err
	}
	hooks := asMap(settings["hooks"])
	marker := "CATACOMB_DISCOVERY="
	for _, ev := range hookEvents {
		entries := pruneCatacomb(asSlice(hooks[ev]), marker)
		if !uninstall {
			entries = append(entries, catacombEntry(discoveryPath, exe, ev))
		}
		if len(entries) == 0 {
			delete(hooks, ev)
		} else {
			hooks[ev] = entries
		}
	}
	if len(hooks) == 0 {
		delete(settings, "hooks")
	} else {
		settings["hooks"] = hooks
	}
	return writeSettings(path, settings)
}

func readSettings(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("install-hooks read: %w", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(b, &settings); err != nil {
		return nil, fmt.Errorf("install-hooks parse: %w", err)
	}
	if settings == nil {
		settings = map[string]any{}
	}
	return settings, nil
}

func writeSettings(path string, settings map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("install-hooks mkdir: %w", err)
	}
	b, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("install-hooks marshal: %w", err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		return fmt.Errorf("install-hooks write: %w", err)
	}
	return nil
}

func catacombEntry(discoveryPath, exe, event string) map[string]any {
	return map[string]any{
		"matcher": "",
		"hooks": []any{map[string]any{
			"type":    "command",
			"command": fmt.Sprintf("CATACOMB_DISCOVERY=%s %s hook %s", discoveryPath, exe, event),
		}},
	}
}

func pruneCatacomb(entries []any, marker string) []any {
	out := make([]any, 0, len(entries))
	for _, e := range entries {
		if !entryHasMarker(e, marker) {
			out = append(out, e)
		}
	}
	return out
}

func entryHasMarker(entry any, marker string) bool {
	m, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	for _, h := range asSlice(m["hooks"]) {
		hm, ok := h.(map[string]any)
		if !ok {
			continue
		}
		if cmd, ok := hm["command"].(string); ok && strings.Contains(cmd, marker) {
			return true
		}
	}
	return false
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func asSlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}
