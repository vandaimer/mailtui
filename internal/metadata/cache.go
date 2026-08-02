// Package metadata stores read-only Maildir header summaries outside the
// backup. It is an acceleration cache, never a source of truth.
package metadata

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"mailtui/internal/message"
)

const cacheVersion = 1

type Store struct{ dir string }

type cacheFile struct {
	Version     int             `json:"version"`
	Folder      string          `json:"folder"`
	Fingerprint string          `json:"fingerprint"`
	Messages    []cachedMessage `json:"messages"`
}

type cachedMessage struct {
	Path      string    `json:"path"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Cc        string    `json:"cc,omitempty"`
	Bcc       string    `json:"bcc,omitempty"`
	Subject   string    `json:"subject"`
	MessageID string    `json:"message_id,omitempty"`
	Date      time.Time `json:"date"`
	DateText  string    `json:"date_text"`
	Error     string    `json:"error,omitempty"`
}

func New(root string) *Store {
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		return &Store{}
	}
	rootHash := sha256.Sum256([]byte(filepath.Clean(root)))
	return &Store{dir: filepath.Join(base, "mailtui", "metadata-v1", hex.EncodeToString(rootHash[:12]))}
}

func NewAt(dir string) *Store { return &Store{dir: dir} }

func (store *Store) Load(folder, fingerprint string) ([]message.Message, bool) {
	if store == nil || store.dir == "" {
		return nil, false
	}
	data, err := os.ReadFile(store.filePath(folder))
	if err != nil {
		return nil, false
	}
	var cached cacheFile
	if json.Unmarshal(data, &cached) != nil || cached.Version != cacheVersion || cached.Folder != folder || cached.Fingerprint != fingerprint {
		return nil, false
	}
	messages := make([]message.Message, 0, len(cached.Messages))
	for _, item := range cached.Messages {
		parsed := message.Message{
			Path: item.Path, From: item.From, To: item.To, Cc: item.Cc, Bcc: item.Bcc,
			Subject: item.Subject, MessageID: item.MessageID, Date: item.Date, DateText: item.DateText,
		}
		if item.Error != "" {
			parsed.Err = errors.New(item.Error)
		}
		messages = append(messages, parsed)
	}
	return messages, true
}

func (store *Store) Save(folder, fingerprint string, messages []message.Message) error {
	if store == nil || store.dir == "" {
		return nil
	}
	if err := os.MkdirAll(store.dir, 0o700); err != nil {
		return err
	}
	cached := cacheFile{Version: cacheVersion, Folder: folder, Fingerprint: fingerprint, Messages: make([]cachedMessage, 0, len(messages))}
	for _, item := range messages {
		entry := cachedMessage{
			Path: item.Path, From: item.From, To: item.To, Cc: item.Cc, Bcc: item.Bcc,
			Subject: item.Subject, MessageID: item.MessageID, Date: item.Date, DateText: item.DateText,
		}
		if item.Err != nil {
			entry.Error = item.Err.Error()
		}
		cached.Messages = append(cached.Messages, entry)
	}
	data, err := json.Marshal(cached)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(store.dir, ".metadata-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, store.filePath(folder))
}

func (store *Store) filePath(folder string) string {
	hash := sha256.Sum256([]byte(filepath.Clean(folder)))
	return filepath.Join(store.dir, hex.EncodeToString(hash[:])+".json")
}
