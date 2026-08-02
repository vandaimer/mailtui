// Package config loads mailtui's optional user configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const fileName = "config.toml"

// Config is the user configuration stored outside the Maildir.
type Config struct {
	Maildir string `toml:"maildir"`
}

// FilePath returns the platform-specific path to mailtui's configuration file.
func FilePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find user configuration directory: %w", err)
	}
	return filepath.Join(dir, "mailtui", fileName), nil
}

// LoadFile reads and validates a TOML configuration file.
func LoadFile(path string) (Config, error) {
	var cfg Config
	metadata, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		return Config{}, err
	}
	if undecoded := metadata.Undecoded(); len(undecoded) > 0 {
		return Config{}, fmt.Errorf("unknown configuration key %q", undecoded[0].String())
	}
	if strings.TrimSpace(cfg.Maildir) == "" {
		return Config{}, fmt.Errorf("maildir must not be empty")
	}
	return cfg, nil
}

// ExpandHome expands a leading "~/" in a configured path.
func ExpandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find user home directory: %w", err)
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, filepath.FromSlash(strings.TrimPrefix(path, "~/"))), nil
}
