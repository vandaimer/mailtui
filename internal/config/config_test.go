package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilePathUsesUserConfigDirectory(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)

	path, err := FilePath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(configDir, "mailtui", "config.toml")
	if path != want {
		t.Fatalf("FilePath() = %q, want %q", path, want)
	}
}

func TestLoadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("maildir = \"~/mail backup\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Maildir != "~/mail backup" {
		t.Fatalf("Maildir = %q, want %q", cfg.Maildir, "~/mail backup")
	}
}

func TestLoadFileRejectsUnknownKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("mail_dir = \"/mail\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFile(path)
	if err == nil || !strings.Contains(err.Error(), "unknown configuration key") {
		t.Fatalf("LoadFile() error = %v, want unknown key error", err)
	}
}

func TestLoadFileRequiresMaildir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("maildir = \"  \"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFile(path)
	if err == nil || !strings.Contains(err.Error(), "maildir must not be empty") {
		t.Fatalf("LoadFile() error = %v, want empty maildir error", err)
	}
}

func TestExpandHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := ExpandHome("~/mail/backup")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "mail", "backup")
	if got != want {
		t.Fatalf("ExpandHome() = %q, want %q", got, want)
	}
}
