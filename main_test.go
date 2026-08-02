package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveRootPrefersCommandLineArgument(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	explicit := filepath.Join(t.TempDir(), "mail")

	got, err := resolveRoot([]string{explicit})
	if err != nil {
		t.Fatal(err)
	}
	if got != explicit {
		t.Fatalf("resolveRoot() = %q, want %q", got, explicit)
	}
}

func TestResolveRootUsesConfigurationWithoutArgument(t *testing.T) {
	configDir := t.TempDir()
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv("HOME", home)
	path := filepath.Join(configDir, "mailtui", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("maildir = \"~/mail\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := resolveRoot(nil)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "mail")
	if got != want {
		t.Fatalf("resolveRoot() = %q, want %q", got, want)
	}
}

func TestResolveRootExplainsMissingConfiguration(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)

	_, err := resolveRoot(nil)
	if err == nil || !strings.Contains(err.Error(), filepath.Join(configDir, "mailtui", "config.toml")) {
		t.Fatalf("resolveRoot() error = %v, want configuration path", err)
	}
}
