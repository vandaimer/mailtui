package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateReplacesExecutableAfterChecksumVerification(t *testing.T) {
	asset := "mailtui_linux_amd64.tar.gz"
	wantBinary := []byte("new mailtui binary")
	archive := releaseArchive(t, wantBinary)
	checksum := sha256.Sum256(archive)
	server := releaseServer(t, asset, archive, fmt.Sprintf("%x  %s\n", checksum, asset))
	defer server.Close()

	target := filepath.Join(t.TempDir(), "mailtui")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err := update(context.Background(), options{
		apiBaseURL: server.URL, downloadBaseURL: server.URL,
		repository: "acme/mailtui", target: target, currentVersion: "v1.0.0",
		goos: "linux", goarch: "amd64", client: server.Client(), output: &output,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, wantBinary) {
		t.Fatalf("installed binary = %q", got)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("installed mode = %v", info.Mode().Perm())
	}
	if !strings.Contains(output.String(), "checksum verified") || !strings.Contains(output.String(), "v1.2.3 installed") {
		t.Fatalf("unexpected output:\n%s", output.String())
	}
}

func TestUpdateDoesNotReplaceExecutableOnChecksumFailure(t *testing.T) {
	asset := "mailtui_linux_amd64.tar.gz"
	archive := releaseArchive(t, []byte("new binary"))
	server := releaseServer(t, asset, archive, strings.Repeat("0", 64)+"  "+asset+"\n")
	defer server.Close()

	target := filepath.Join(t.TempDir(), "mailtui")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := update(context.Background(), options{
		apiBaseURL: server.URL, downloadBaseURL: server.URL,
		repository: "acme/mailtui", target: target, currentVersion: "v1.0.0",
		goos: "linux", goarch: "amd64", client: server.Client(),
	})
	if err == nil || !strings.Contains(err.Error(), "checksum verification failed") {
		t.Fatalf("update error = %v", err)
	}
	got, readErr := os.ReadFile(target)
	if readErr != nil || string(got) != "old binary" {
		t.Fatalf("target changed after failed verification: %q, %v", got, readErr)
	}
}

func TestUpdateSkipsDownloadWhenAlreadyCurrent(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		if request.URL.Path != "/repos/acme/mailtui/releases/latest" {
			t.Fatalf("unexpected download: %s", request.URL.Path)
		}
		fmt.Fprint(response, `{"tag_name":"v1.2.3"}`)
	}))
	defer server.Close()

	var output bytes.Buffer
	err := update(context.Background(), options{
		apiBaseURL: server.URL, downloadBaseURL: server.URL,
		repository: "acme/mailtui", target: filepath.Join(t.TempDir(), "mailtui"), currentVersion: "v1.2.3",
		goos: "linux", goarch: "amd64", client: server.Client(), output: &output,
	})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 || !strings.Contains(output.String(), "already up to date") {
		t.Fatalf("requests = %d, output = %q", requests, output.String())
	}
}

func releaseArchive(t *testing.T, binary []byte) []byte {
	t.Helper()
	var output bytes.Buffer
	gzipWriter := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "mailtui", Mode: 0o755, Size: int64(len(binary))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func releaseServer(t *testing.T, asset string, archive []byte, checksums string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/repos/acme/mailtui/releases/latest":
			fmt.Fprint(response, `{"tag_name":"v1.2.3"}`)
		case "/acme/mailtui/releases/download/v1.2.3/" + asset:
			response.Write(archive)
		case "/acme/mailtui/releases/download/v1.2.3/checksums.txt":
			fmt.Fprint(response, checksums)
		default:
			http.NotFound(response, request)
		}
	}))
}
