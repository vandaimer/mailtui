// Package maildir discovers and reads Maildir folders. It intentionally exposes
// no write operations.
package maildir

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"mailtui/internal/message"
)

type Folder struct {
	Path     string
	Name     string
	Messages []message.Message
}

func Discover(root string) ([]Folder, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("não é um diretório: %s", root)
	}

	var result []Folder
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			return nil
		}
		if IsMaildir(path) {
			name, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			if name == "." {
				name = filepath.Base(root)
			}
			result = append(result, Folder{Path: path, Name: name})
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	SortFolders(result)
	return result, nil
}

func IsMaildir(path string) bool {
	for _, name := range []string{"cur", "new", "tmp"} {
		info, err := os.Stat(filepath.Join(path, name))
		if err != nil || !info.IsDir() {
			return false
		}
	}
	return true
}

func Load(folder *Folder) error {
	if folder.Messages != nil {
		return nil
	}
	folder.Messages = []message.Message{}
	var errs []error
	for _, bucket := range []string{"cur", "new"} {
		entries, err := os.ReadDir(filepath.Join(folder.Path, bucket))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			path := filepath.Join(folder.Path, bucket, entry.Name())
			parsed, parseErr := message.ParseFile(path)
			if parseErr != nil {
				parsed = message.Message{Path: path, Subject: "[mensagem inválida]", Err: parseErr}
				errs = append(errs, fmt.Errorf("%s: %w", path, parseErr))
			}
			folder.Messages = append(folder.Messages, parsed)
		}
	}
	sort.SliceStable(folder.Messages, func(i, j int) bool {
		return folder.Messages[i].Date.After(folder.Messages[j].Date)
	})
	return errors.Join(errs...)
}

func SortFolders(folders []Folder) {
	sort.SliceStable(folders, func(i, j int) bool {
		leftRank, rightRank := folderRank(folders[i].Name), folderRank(folders[j].Name)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return naturalLess(folders[i].Name, folders[j].Name)
	})
}

func naturalLess(left, right string) bool {
	a, b := []rune(strings.ToLower(left)), []rune(strings.ToLower(right))
	for len(a) > 0 && len(b) > 0 {
		if unicode.IsDigit(a[0]) && unicode.IsDigit(b[0]) {
			ai, bi := 0, 0
			for ai < len(a) && unicode.IsDigit(a[ai]) {
				ai++
			}
			for bi < len(b) && unicode.IsDigit(b[bi]) {
				bi++
			}
			an, _ := strconv.ParseUint(string(a[:ai]), 10, 64)
			bn, _ := strconv.ParseUint(string(b[:bi]), 10, 64)
			if an != bn {
				return an < bn
			}
			a, b = a[ai:], b[bi:]
			continue
		}
		if a[0] != b[0] {
			return a[0] < b[0]
		}
		a, b = a[1:], b[1:]
	}
	return len(a) < len(b)
}

func folderRank(name string) int {
	normalized := strings.ToLower(strings.TrimSpace(filepath.ToSlash(name)))
	if normalized == "inbox" || strings.HasSuffix(normalized, "/inbox") {
		return 0
	}
	base := strings.Trim(strings.ToLower(filepath.Base(normalized)), "[]")
	if strings.HasPrefix(normalized, "[gmail]") || strings.HasPrefix(normalized, "gmail") {
		return 1
	}
	for _, system := range []string{"all mail", "sent", "sent mail", "drafts", "important", "starred", "spam", "trash"} {
		if base == system {
			return 1
		}
	}
	return 2
}

func DisplayName(name string) string {
	return strings.ReplaceAll(filepath.ToSlash(name), "/", " › ")
}
