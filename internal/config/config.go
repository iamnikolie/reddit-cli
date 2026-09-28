// Package config loads the per-profile configuration from ~/.reddit-cli.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// DefaultArchiveURL is the Arctic Shift API root; REDDIT_ARCHIVE_URL overrides it.
const DefaultArchiveURL = "https://arctic-shift.photon-reddit.com"

// DefaultProfile is used when neither --config nor REDDIT_CONFIG names one.
const DefaultProfile = "default"

// Config is one profile. Every field is optional: the archive source needs no
// credentials. The Reddit OAuth fields are reserved for the oauth source.
type Config struct {
	UserAgent  string `yaml:"user_agent,omitempty"`
	ArchiveURL string `yaml:"archive_url,omitempty"`

	ClientID     string `yaml:"client_id,omitempty"`
	ClientSecret string `yaml:"client_secret,omitempty"`
	Username     string `yaml:"username,omitempty"`
	Password     string `yaml:"password,omitempty"`
}

// home returns the config dir for a profile. REDDIT_CLI_HOME overrides the
// base (test escape hatch); the default base is ~/.reddit-cli.
func home(profile string) (string, error) {
	base := os.Getenv("REDDIT_CLI_HOME")
	if base == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(h, ".reddit-cli")
	}
	if profile == "" {
		profile = DefaultProfile
	}
	return filepath.Join(base, profile), nil
}

// Dir returns the profile directory (state files live next to config.yaml).
func Dir(profile string) (string, error) { return home(profile) }

// Path returns the config file path for a profile.
func Path(profile string) (string, error) {
	dir, err := home(profile)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// Load reads the profile's config file; a missing file is not an error.
// REDDIT_ARCHIVE_URL and REDDIT_USER_AGENT override the file values.
func Load(profile string) (*Config, error) {
	cfg := &Config{}
	path, err := Path(profile)
	if err != nil {
		return nil, fmt.Errorf("config.Load: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("config.Load: %w", err)
	}
	if err == nil {
		if e := yaml.Unmarshal(data, cfg); e != nil {
			return nil, fmt.Errorf("config.Load: parse %s: %w", path, e)
		}
	}
	if env := os.Getenv("REDDIT_ARCHIVE_URL"); env != "" {
		cfg.ArchiveURL = env
	}
	if env := os.Getenv("REDDIT_USER_AGENT"); env != "" {
		cfg.UserAgent = env
	}
	if cfg.ArchiveURL == "" {
		cfg.ArchiveURL = DefaultArchiveURL
	}
	return cfg, nil
}

// Save writes the config to ~/.reddit-cli/<profile>/config.yaml (dir 0700,
// file 0600 because the file may hold OAuth credentials).
func Save(cfg *Config, profile string) (string, error) {
	path, err := Path(profile)
	if err != nil {
		return "", fmt.Errorf("config.Save: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("config.Save: mkdir: %w", err)
	}
	out := *cfg
	if out.ArchiveURL == DefaultArchiveURL {
		out.ArchiveURL = ""
	}
	data, err := yaml.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("config.Save: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("config.Save: %w", err)
	}
	return path, nil
}
