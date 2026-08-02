package main

import (
	"bytes"
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

func TestRunWithoutConfigurationShowsFriendlyGuide(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	var stdout, stderr bytes.Buffer

	if err := runWithIO(nil, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"not configured yet", filepath.Join(configDir, "mailtui", "config.toml"), "mailtui --config FILE", "mailtui update"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("guide missing %q:\n%s", want, stdout.String())
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}

func TestHelpAndConfigCommandsExplainConfiguration(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	path := filepath.Join(configDir, "mailtui", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("maildir = \"/backup/mail\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var help bytes.Buffer
	if err := runWithIO([]string{"help"}, &help, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(help.String(), path) || !strings.Contains(help.String(), "maildir = \"/path/to/your/mail\"") {
		t.Fatalf("unexpected help:\n%s", help.String())
	}

	var shown bytes.Buffer
	if err := runWithIO([]string{"config"}, &shown, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(shown.String(), path) || !strings.Contains(shown.String(), `maildir = "/backup/mail"`) {
		t.Fatalf("unexpected config output:\n%s", shown.String())
	}
}

func TestResolveRootUsesSpecificConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "work.toml")
	if err := os.WriteFile(path, []byte("maildir = \"/work/mail\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := resolveRootWithConfig(nil, path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/work/mail" {
		t.Fatalf("root = %q", got)
	}
}
