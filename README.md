# mailtui

A fast, offline, **read-only** TUI for browsing email backups stored in Maildir
format. The root may contain multiple folders or labels; every directory with
`cur/`, `new/`, and `tmp/` is discovered automatically.

## Usage

```sh
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o mailtui .
./mailtui /mnt/mail
```

Install a published release on Linux or macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/OWNER/mailtui/main/install.sh | \
  sh -s -- --repo OWNER/mailtui
```

The installer detects the operating system and architecture, verifies the
release checksum, and installs to `~/.local/bin` by default. Use `--dir PATH`,
`--version vX.Y.Z`, or the corresponding `MAILTUI_*` environment variables to
override its settings.

The build produces a single static executable with no project runtime or
libraries to install. The application does not access Gmail, OAuth, IMAP, or
SMTP. It only lists directories and reads messages from `cur/` and `new/`;
`tmp/` is used solely to recognize the Maildir structure.

On wide terminals, the interface displays folders, messages, and the selected
email preview at the same time. On medium widths, the message list and reader
are stacked. On narrow terminals, each pane takes over the screen to remain
legible.

Keys:

- `↑/↓` or `j/k`: navigate the focused pane;
- `Tab`, `Shift+Tab`, `←/→`, or `h/l`: change focus;
- `/`: search by subject, sender, or recipients;
- `Enter`: apply the search or move to the next pane;
- `Esc`: cancel search, clear the filter, or go back;
- `PgUp/PgDn`: scroll the message body;
- `o`: select and open an attachment with the default application;
- `q`: quit.

`INBOX` is listed first, followed by Gmail/system folders and user labels in
natural order. The reader displays important headers, the `text/plain` body
(with a basic HTML-to-text fallback), and MIME attachment metadata. Unreadable
messages remain visible as invalid entries, which helps verify backup integrity
without interrupting navigation.

## Network backups

Navigation never waits for filesystem I/O in the interface event loop. When a
folder is selected, mailtui reads only message headers with bounded concurrency
and keeps the results in memory. The complete MIME body—including attachment
payloads—is read only for the selected message. Selection is debounced so that
moving quickly through folders or messages does not trigger unnecessary reads
from a remote mount.

During the first folder scan, headers appear progressively in batches. When the
scan completes, mailtui stores only those metadata summaries under
`${XDG_CACHE_HOME:-~/.cache}/mailtui/metadata-v1/`. On later runs it compares
the Maildir filename list and reuses the cache when nothing changed. No index or
cache is ever created inside the backup.

## Attachments

Once a message has loaded, press `o`, select an attachment, and press `Enter`.
The payload is decoded into
`${XDG_CACHE_HOME:-~/.cache}/mailtui/attachments/` with restricted permissions
and opened through the platform's default application. For example, a PDF opens
in the default PDF viewer. Original Maildir files remain untouched.

## Releases

Every pushed `vX.Y.Z` tag runs the release workflow, tests the project, builds
static binaries for Linux, macOS, and Windows on amd64 and arm64, writes SHA-256
checksums, and creates a GitHub release with generated notes.

```sh
git tag v0.1.0
git push origin v0.1.0
```

The regular CI workflow checks formatting, runs the race-enabled test suite and
`go vet`, and verifies a static build on every push and pull request.
