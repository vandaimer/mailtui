// Package attachment safely materializes MIME attachments outside the Maildir
// and opens them with the desktop's default application.
package attachment

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"mailtui/internal/message"
)

func ExtractToCache(messagePath string, index int) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		return "", errors.New("user cache directory is unavailable")
	}
	return ExtractTo(messagePath, index, filepath.Join(base, "mailtui", "attachments"))
}

func ExtractTo(messagePath string, index int, destination string) (string, error) {
	item, payload, err := message.ExtractAttachment(messagePath, index)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return "", err
	}
	fingerprint := sha256.Sum256([]byte(messagePath + "\x00" + strconv.Itoa(index)))
	name := hex.EncodeToString(fingerprint[:6]) + "-" + safeName(item.Name)
	path := filepath.Join(destination, name)
	temporary, err := os.CreateTemp(destination, ".attachment-*")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return "", err
	}
	if _, err := temporary.Write(payload); err != nil {
		temporary.Close()
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return "", err
	}
	return path, nil
}

func OpenDefault(path string) error {
	binary, err := exec.LookPath("xdg-open")
	if err != nil {
		return errors.New("xdg-open is not installed")
	}
	command := exec.Command(binary, path)
	if err := command.Start(); err != nil {
		return err
	}
	go func() { _ = command.Wait() }()
	return nil
}

func safeName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(value rune) rune {
		if unicode.IsControl(value) || value == '/' || value == '\\' {
			return '_'
		}
		return value
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." {
		return "unnamed-attachment"
	}
	runes := []rune(name)
	if len(runes) > 180 {
		name = string(runes[:180])
	}
	return name
}
