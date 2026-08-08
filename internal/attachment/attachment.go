// Package attachment safely materializes MIME attachments outside the Maildir
// and opens them with the desktop's default application.
package attachment

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"unicode"

	"mailtui/internal/message"
)

// OpenResult describes the artifact created by an attachment-opening request.
// Path remains available when the desktop application could not be started, so
// callers can tell the user where to open the materialized attachment.
type OpenResult struct {
	Path string
}

// Open decodes one MIME attachment, writes it to the user cache, and starts
// the platform default application. It never writes to the message or its
// Maildir. If opening fails after materialization, the result still contains
// the retained cache path.
func Open(messagePath string, index int) (OpenResult, error) {
	destination, err := cacheDirectory()
	if err != nil {
		return OpenResult{}, err
	}
	return open(messagePath, index, destination, systemCommandRunner{})
}

func cacheDirectory() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		return "", errors.New("user cache directory is unavailable")
	}
	return filepath.Join(base, "mailtui", "attachments"), nil
}

// commandRunner is intentionally internal: production has one platform
// opener, while tests can deterministically observe whether it was started.
type commandRunner interface {
	Start(name string, args ...string) error
}

type systemCommandRunner struct{}

func (systemCommandRunner) Start(name string, args ...string) error {
	binary, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("%s is not installed", name)
	}
	command := exec.Command(binary, args...)
	if err := command.Start(); err != nil {
		return err
	}
	go func() { _ = command.Wait() }()
	return nil
}

// open is the workflow seam for package tests. Keeping the cache destination
// and command runner here makes the public Open interface one operation.
func open(messagePath string, index int, destination string, runner commandRunner) (OpenResult, error) {
	path, err := materialize(messagePath, index, destination)
	if err != nil {
		return OpenResult{}, err
	}
	result := OpenResult{Path: path}
	name, args, err := openerCommand(path)
	if err != nil {
		return result, err
	}
	if err := runner.Start(name, args...); err != nil {
		return result, err
	}
	return result, nil
}

func materialize(messagePath string, index int, destination string) (string, error) {
	if err := rejectMaildirDestination(messagePath, destination); err != nil {
		return "", err
	}
	item, payload, err := message.ExtractAttachment(messagePath, index)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(destination, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(destination, materializedName(messagePath, index, item.Name))
	temporary, err := os.CreateTemp(destination, ".attachment-*")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return "", err
	}
	if _, err := temporary.Write(payload); err != nil {
		_ = temporary.Close()
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

func rejectMaildirDestination(messagePath, destination string) error {
	messageDirectory := filepath.Dir(messagePath)
	if base := filepath.Base(messageDirectory); base != "cur" && base != "new" {
		return nil
	}
	maildirRoot := filepath.Dir(messageDirectory)
	relative, err := filepath.Rel(maildirRoot, destination)
	if err != nil {
		return err
	}
	if relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("attachment cache directory must be outside the Maildir")
	}
	return nil
}

func materializedName(messagePath string, index int, attachmentName string) string {
	fingerprint := sha256.Sum256([]byte(messagePath + "\x00" + strconv.Itoa(index)))
	return hex.EncodeToString(fingerprint[:16]) + "-" + safeName(attachmentName)
}

func openerCommand(path string) (string, []string, error) {
	switch runtime.GOOS {
	case "linux":
		return "xdg-open", []string{path}, nil
	case "darwin":
		return "open", []string{path}, nil
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", path}, nil
	default:
		return "", nil, fmt.Errorf("opening attachments is not supported on %s", runtime.GOOS)
	}
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
	if name == "" || name == "." || name == ".." {
		return "unnamed-attachment"
	}
	return truncateName(name, 160)
}

func truncateName(name string, maximumBytes int) string {
	if len(name) <= maximumBytes {
		return name
	}
	end := 0
	for index := range name {
		if index > maximumBytes {
			break
		}
		end = index
	}
	return name[:end]
}
