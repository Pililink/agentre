package codex

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

const legacyPriorityServiceTierOverride = `service_tier="fast"`

func appendLegacyPriorityServiceTierOverride(config []string, env map[string]string) []string {
	if hasConfigKey(config, "service_tier") || !usesLegacyPriorityServiceTier(env) {
		return config
	}
	out := append([]string{}, config...)
	return append(out, legacyPriorityServiceTierOverride)
}

func hasConfigKey(config []string, key string) bool {
	for _, expr := range config {
		left, _, ok := strings.Cut(strings.TrimSpace(expr), "=")
		if ok && strings.TrimSpace(left) == key {
			return true
		}
	}
	return false
}

func usesLegacyPriorityServiceTier(env map[string]string) bool {
	path := codexConfigPath(env)
	if path == "" {
		return false
	}
	//nolint:gosec // Reading the user's Codex config locally to add a process-scoped compatibility override.
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if beforeComment, _, ok := strings.Cut(line, "#"); ok {
			line = strings.TrimSpace(beforeComment)
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "service_tier" {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		return value == "priority"
	}
	return false
}

func codexConfigPath(env map[string]string) string {
	if home := strings.TrimSpace(env["CODEX_HOME"]); home != "" {
		return filepath.Join(home, "config.toml")
	}
	if home := strings.TrimSpace(os.Getenv("CODEX_HOME")); home != "" {
		return filepath.Join(home, "config.toml")
	}
	userHome, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(userHome) == "" {
		return ""
	}
	return filepath.Join(userHome, ".codex", "config.toml")
}
