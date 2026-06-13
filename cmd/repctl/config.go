package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config holds how to reach a RePanel instance. It is loaded from (lowest to
// highest precedence) the config file, environment variables, then flags.
type Config struct {
	URL      string `json:"url"`
	Token    string `json:"token"`
	Insecure bool   `json:"insecure,omitempty"`
}

// configPath returns the path to the CLI config file, honouring REPANEL_CONFIG.
func configPath() string {
	if p := os.Getenv("REPANEL_CONFIG"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "repanel", "cli.json")
}

// loadConfig reads the config file; a missing or unreadable file yields a zero
// Config, not an error (env/flags may still supply everything needed).
func loadConfig() Config {
	var c Config
	if b, err := os.ReadFile(configPath()); err == nil {
		json.Unmarshal(b, &c)
	}
	return c
}

// save writes the config file with 0600 permissions (it contains a token).
func (c Config) save() error {
	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// mergeEnv overlays REPANEL_URL / REPANEL_TOKEN / REPANEL_INSECURE onto c.
func (c Config) mergeEnv() Config {
	if v := os.Getenv("REPANEL_URL"); v != "" {
		c.URL = v
	}
	if v := os.Getenv("REPANEL_TOKEN"); v != "" {
		c.Token = v
	}
	if v := os.Getenv("REPANEL_INSECURE"); v == "1" || v == "true" {
		c.Insecure = true
	}
	return c
}
