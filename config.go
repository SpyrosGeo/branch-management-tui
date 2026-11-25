package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	BaseURL string
	Email   string
	Token   string
	JQL     string
	// action id -> key, e.g. {"copy-branch": "c"}
	Keymap map[string]string
}

type fileConfig struct {
	BaseURL string            `json:"baseUrl"`
	JQL     string            `json:"jql"`
	Keymap  map[string]string `json:"keymap"`
}

const DefaultJQL = `assignee = currentUser() AND statusCategory != Done ORDER BY updated DESC`

func configPath(getenv func(string) string) string {
	dir := getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "branch-tui", "config.json")
}

func readFileConfig(path string) (fileConfig, error) {
	var c fileConfig
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("invalid JSON in %s: %w", path, err)
	}
	return c, nil
}

// loadConfig takes secrets only from env; the config file holds non-secret settings.
func loadConfig(getenv func(string) string, path string) (Config, error) {
	file, err := readFileConfig(path)
	if err != nil {
		return Config{}, err
	}
	baseURL := getenv("JIRA_BASE_URL")
	if baseURL == "" {
		baseURL = file.BaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	var missing []string
	if baseURL == "" {
		missing = append(missing, fmt.Sprintf(`JIRA_BASE_URL (or "baseUrl" in %s)`, path))
	}
	for _, k := range []string{"JIRA_EMAIL", "JIRA_API_TOKEN"} {
		if getenv(k) == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing configuration: %s", strings.Join(missing, ", "))
	}
	c := Config{BaseURL: baseURL, Email: getenv("JIRA_EMAIL"), Token: getenv("JIRA_API_TOKEN"), JQL: file.JQL, Keymap: file.Keymap}
	if c.JQL == "" {
		c.JQL = DefaultJQL
	}
	if c.Keymap == nil {
		c.Keymap = map[string]string{}
	}
	return c, nil
}
