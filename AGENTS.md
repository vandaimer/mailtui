# AGENTS.md — mailtui

## Project purpose

`mailtui` is a beautiful, fast, offline, read-only TUI for inspecting Gmail
backups stored as Maildir. Its primary job is to let the user navigate the
backup comfortably and gain confidence that folders, recent messages, message
bodies, and attachments were downloaded successfully.

The application must never connect to Gmail and must never alter the backup.

## Non-negotiable constraints

- Production code must treat the Maildir root as strictly read-only.
- Never move, rename, delete, rewrite, chmod, or change flags on message files.
- Do not create Maildir folders, lock files, indexes, or caches inside the mail
  root. Any future index must live in a separate user data/cache directory.
- Read messages as local binary files from `cur/` and `new/`. Use `tmp/` only
  when recognizing a Maildir.
- No sending, replying, forwarding, moving, deleting, flag editing, IMAP,
  SMTP, OAuth, or remote Gmail access.
- The distributable must remain one native executable. Go is the chosen stack.
- Do not commit the generated `/mailtui` binary; build it locally or publish it
  as a release artifact.

## Current stack and commands

- Go 1.26.1 (managed by `mise.toml`)
- Bubble Tea for the terminal event loop
- Go standard library for Maildir discovery and RFC 822/MIME parsing

```sh
go test ./...
go vet ./...
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o mailtui .
./mailtui /path/to/mail
```

The static Linux amd64 build is currently about 3.5 MB.

## Current implementation

The code is split across `internal/maildir`, `internal/message`, and
`internal/ui`, with the CLI in `main.go`. It currently provides:

- recursive discovery of directories containing `cur/`, `new/`, and `tmp/`;
- lazy loading of messages when a folder is opened;
- descending date sort inside a folder;
- decoding of encoded headers;
- `text/plain` display with a simple HTML-to-text fallback;
- Base64 and quoted-printable transfer decoding;
- display of From, To, Cc, Bcc, Subject, Date, and Message-ID;
- attachment name, MIME type, and decoded size;
- visible invalid-message entries instead of aborting a folder load;
- semantic folder ordering with INBOX and Gmail/system folders first;
- a styled responsive three/two/one-pane master-detail interface;
- automatic selected-message previews and compact message snippets;
- keyboard navigation and `/` filtering over subject and address headers.
- asynchronous, debounced network I/O: concurrent header-only folder scans and
  full MIME hydration only for the selected message.
- progressive header batches and a fingerprinted metadata cache under the
  user's cache directory for fast repeat visits;
- safe on-demand attachment extraction outside Maildir and opening through
  the platform default application from the attachment picker;
- release automation for Linux, macOS, and Windows, plus a checksum-verifying
  installer for Linux and macOS.

Tests alongside each internal package cover Maildir discovery and ordering,
MIME parsing, attachments, HTML fallback, Base64 bodies, search interaction,
selection-driven preview, and responsive rendering decisions.

## I/O and performance architecture

Maildirs may live on SMB/GVFS/network mounts. Never perform filesystem I/O from
the Bubble Tea `Update` path. Folder selection emits a debounced command;
`maildir.ScanHeaders` reads only RFC 822 headers with a fixed 12-worker limit.
The UI stores those summaries in memory. Message selection emits a separate
debounced command and calls `message.ParseFile` only for that one file. Loaded
messages remain hydrated in memory while the application runs.

On cache miss, headers are delivered to the UI in batches of 64. At completion,
summaries are written to the platform user cache directory. Cache validation
hashes the sorted `cur/` and `new/` paths, so a normal Maildir filename change
invalidates it without reading message contents. Cache files contain headers,
not bodies or attachment payloads.

Do not regress to `os.ReadFile` for every message in a folder. File managers
appear fast because they list directory entries without parsing mail; this
two-phase strategy keeps mail-specific metadata while avoiding full attachment
downloads. A future persistent index must still live outside the Maildir.

Attachment opening is explicitly user-triggered with `o`. The selected MIME
part is decoded again, sanitized with `filepath.Base`, written with mode 0600
under the user cache, and handed to the platform opener. Never materialize
attachments under the Maildir or trust a MIME filename as a path.

## Current UX status

The first redesign milestone is implemented. Real-backup evaluation is now the
most important input: polish density, colors, sizing, focus behavior, snippets,
and edge cases based on actual use rather than returning to a full-screen list.

## Desired UX direction

Build a responsive master-detail mail reader inspired by good desktop mail
clients while remaining terminal-native:

1. Keep folders visible in a narrow left pane.
2. Show messages in a middle pane with compact, useful previews (sender,
   subject, date/time, and ideally a body snippet).
3. Show the selected message immediately in a larger right/bottom preview pane;
   Enter may expand/focus the reader rather than being required just to see it.
4. On narrow terminals, collapse gracefully to two panes or the existing
   drill-down navigation.
5. Use borders, spacing, restrained color, focus states, selected-row styling,
   counts, titles, and a concise contextual help/status bar. The result should
   look intentionally designed, not like debug output.
6. Preserve fast keyboard navigation and avoid loading the entire backup into
   memory at startup.

Folder ordering should be deliberate rather than plain alphabetical:

- `INBOX` always comes first.
- Gmail/system folders (names beginning with Gmail or commonly represented as
  `[Gmail]`, including Sent, Drafts, All Mail, Spam, Trash, and Starred) come
  next in a coherent group.
- User labels follow, sorted naturally and case-insensitively.
- Nested labels should be visually understandable; avoid exposing awkward raw
  path structure when a friendly label can be shown.

Search is a high-priority feature. `/` should enter a visible search mode, with
clear input, result count, Esc to cancel, and navigation through results. Start
with folder-local filtering over subject, sender, and recipients. Design the
model so body search and a separate SQLite index can be added later without
writing anywhere inside the Maildir.

## Current package structure

```text
main.go                 CLI entry point
internal/maildir/       discovery, ordering, and read-only filesystem access
internal/message/       RFC 822/MIME parsing and view models
internal/ui/            Bubble Tea model, panes, styles, search, key handling
```

## Near-term execution order

1. Evaluate the redesigned UI against a real large backup and collect concrete
   visual/interaction feedback.
2. Test with a synthetic Maildir containing nested labels, malformed messages,
   multipart alternatives, attachments, long Unicode headers, and many rows.
3. Improve MIME charset handling and HTML conversion where real mail exposes
   deficiencies.
4. Only then consider SQLite full-text indexing, attachment export, external
   opening, configuration files, or richer HTML conversion.

## Definition of done for the redesign milestone

- `mailtui /path/to/mail` still produces no writes under that path.
- INBOX is first and Gmail/system folders form the next logical group.
- The normal desktop-sized view simultaneously communicates folders, message
  list, and selected-message content.
- `/` search is discoverable and useful without reading documentation.
- Focus, selection, loading, empty, parse-error, and no-result states are clear.
- Small-terminal behavior remains usable.
- Existing parsing tests pass and new model/render tests cover ordering, search,
  focus changes, and responsive layout decisions.
- A static `CGO_ENABLED=0` binary still builds successfully.

## Working conventions

- Keep all user-facing text, documentation, code comments, test fixtures, and
  commit messages in English.
- Run `gofmt` on edited Go files and run `go test ./...` before committing.
- Also run `go vet ./...` for meaningful changes.
- Preserve user changes and keep commits focused.
- Prefer tests that use temporary synthetic Maildirs; never test mutations
  against the user's real backup.
- If a design decision is ambiguous, optimize for comfortable inspection of a
  large Gmail backup, strong visual hierarchy, and obvious read-only behavior.
