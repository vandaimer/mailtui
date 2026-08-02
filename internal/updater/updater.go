// Package updater installs a checksum-verified mailtui release over the
// currently running executable. Network access occurs only on an explicit
// `mailtui update` command.
package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	defaultRepository = "vandaimer/mailtui"
	maxDownloadSize   = 256 << 20
	maxBinarySize     = 128 << 20
)

type options struct {
	apiBaseURL      string
	downloadBaseURL string
	repository      string
	target          string
	currentVersion  string
	goos            string
	goarch          string
	client          *http.Client
	output          io.Writer
}

type latestRelease struct {
	TagName string `json:"tag_name"`
}

// Update downloads the latest release for the current platform and replaces
// the running executable after verifying the published checksum.
func Update(ctx context.Context, currentVersion string, output io.Writer) error {
	if runtime.GOOS == "windows" {
		return errors.New("automatic updates are not supported on Windows yet; download the latest release from GitHub")
	}
	repository := os.Getenv("MAILTUI_REPO")
	if repository == "" {
		repository = defaultRepository
	}
	target, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate the current executable: %w", err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(target); resolveErr == nil {
		target = resolved
	}
	return update(ctx, options{
		apiBaseURL:      "https://api.github.com",
		downloadBaseURL: "https://github.com",
		repository:      repository,
		target:          target,
		currentVersion:  currentVersion,
		goos:            runtime.GOOS,
		goarch:          runtime.GOARCH,
		client:          &http.Client{Timeout: 5 * time.Minute},
		output:          output,
	})
}

func update(ctx context.Context, opts options) error {
	if !validRepository(opts.repository) {
		return fmt.Errorf("invalid MAILTUI_REPO %q; expected OWNER/REPOSITORY", opts.repository)
	}
	asset, binaryName, err := releaseAsset(opts.goos, opts.goarch)
	if err != nil {
		return err
	}
	if opts.client == nil {
		opts.client = &http.Client{Timeout: 5 * time.Minute}
	}
	if opts.output == nil {
		opts.output = io.Discard
	}

	tag, err := fetchLatestTag(ctx, opts.client, opts.apiBaseURL, opts.repository)
	if err != nil {
		return err
	}
	if opts.currentVersion == tag {
		_, err = fmt.Fprintf(opts.output, "mailtui %s is already up to date.\n", tag)
		return err
	}

	fmt.Fprintf(opts.output, "Updating mailtui from %s to %s...\n", opts.currentVersion, tag)
	releaseBase := strings.TrimRight(opts.downloadBaseURL, "/") + "/" + opts.repository + "/releases/download/" + url.PathEscape(tag)
	archive, err := download(ctx, opts.client, releaseBase+"/"+asset, maxDownloadSize)
	if err != nil {
		return fmt.Errorf("download %s: %w", asset, err)
	}
	checksums, err := download(ctx, opts.client, releaseBase+"/checksums.txt", 4<<20)
	if err != nil {
		return fmt.Errorf("download checksums.txt: %w", err)
	}
	if err := verifyChecksum(asset, archive, string(checksums)); err != nil {
		return err
	}
	fmt.Fprintln(opts.output, "SHA-256 checksum verified.")

	binary, err := extractBinary(asset, binaryName, archive)
	if err != nil {
		return err
	}
	if err := replaceExecutable(opts.target, binary); err != nil {
		return fmt.Errorf("replace %s: %w; rerun the standard installer if this location is not writable", opts.target, err)
	}
	_, err = fmt.Fprintf(opts.output, "mailtui %s installed to %s\n", tag, opts.target)
	return err
}

func fetchLatestTag(ctx context.Context, client *http.Client, apiBaseURL, repository string) (string, error) {
	endpoint := strings.TrimRight(apiBaseURL, "/") + "/repos/" + repository + "/releases/latest"
	data, err := download(ctx, client, endpoint, 1<<20)
	if err != nil {
		return "", fmt.Errorf("find the latest GitHub release: %w", err)
	}
	var release latestRelease
	if err := json.Unmarshal(data, &release); err != nil {
		return "", fmt.Errorf("decode the latest GitHub release: %w", err)
	}
	if strings.TrimSpace(release.TagName) == "" {
		return "", errors.New("latest GitHub release has no tag")
	}
	return release.TagName, nil
}

func download(ctx context.Context, client *http.Client, source string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "mailtui-updater")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("%s returned %s", source, response.Status)
	}
	if response.ContentLength > limit {
		return nil, fmt.Errorf("response exceeds the %d-byte limit", limit)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeds the %d-byte limit", limit)
	}
	return data, nil
}

func verifyChecksum(asset string, archive []byte, checksums string) error {
	expected := ""
	for line := range strings.Lines(checksums) {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == asset {
			expected = strings.ToLower(fields[0])
			break
		}
	}
	if expected == "" {
		return fmt.Errorf("checksum for %s was not found", asset)
	}
	if _, err := hex.DecodeString(expected); err != nil || len(expected) != sha256.Size*2 {
		return fmt.Errorf("invalid checksum for %s", asset)
	}
	actual := sha256.Sum256(archive)
	if hex.EncodeToString(actual[:]) != expected {
		return fmt.Errorf("checksum verification failed for %s", asset)
	}
	return nil
}

func extractBinary(asset, binaryName string, archive []byte) ([]byte, error) {
	if !strings.HasSuffix(asset, ".tar.gz") {
		return nil, fmt.Errorf("unsupported release archive %s", asset)
	}
	gzipReader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", asset, err)
	}
	defer gzipReader.Close()
	tape := tar.NewReader(gzipReader)
	for {
		header, err := tape.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", asset, err)
		}
		if header.Typeflag != tar.TypeReg || filepath.Clean(header.Name) != binaryName {
			continue
		}
		if header.Size < 1 || header.Size > maxBinarySize {
			return nil, fmt.Errorf("invalid %s size in %s", binaryName, asset)
		}
		binary, err := io.ReadAll(io.LimitReader(tape, maxBinarySize+1))
		if err != nil {
			return nil, fmt.Errorf("extract %s: %w", binaryName, err)
		}
		if int64(len(binary)) != header.Size {
			return nil, fmt.Errorf("truncated %s in %s", binaryName, asset)
		}
		return binary, nil
	}
	return nil, fmt.Errorf("%s was not found in %s", binaryName, asset)
}

func replaceExecutable(target string, binary []byte) error {
	directory := filepath.Dir(target)
	temporary, err := os.CreateTemp(directory, ".mailtui-update-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o755); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(binary); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, target)
}

func releaseAsset(goos, goarch string) (asset, binary string, err error) {
	if goos != "linux" && goos != "darwin" {
		return "", "", fmt.Errorf("automatic updates are not supported on %s", goos)
	}
	if goarch != "amd64" && goarch != "arm64" {
		return "", "", fmt.Errorf("automatic updates are not supported on %s/%s", goos, goarch)
	}
	return fmt.Sprintf("mailtui_%s_%s.tar.gz", goos, goarch), "mailtui", nil
}

func validRepository(repository string) bool {
	parts := strings.Split(repository, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	for _, part := range parts {
		for _, character := range part {
			if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
				(character >= '0' && character <= '9') || character == '-' || character == '_' || character == '.' {
				continue
			}
			return false
		}
	}
	return true
}
