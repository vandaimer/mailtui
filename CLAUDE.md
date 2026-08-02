# CLAUDE.md — mailtui project context

## Mission

Build a polished terminal mail reader for inspecting an offline Gmail backup in
Maildir format. The user runs:

```sh
mailtui /path/to/mail
```

The root contains multiple mailboxes; every nested directory with `cur/`,
`new/`, and `tmp/` is a folder/label. The tool exists to browse those folders
and confirm that the backup contains recent and readable email.

## Safety and packaging

This is a strictly read-only application. Never mutate messages, Maildir flags,
or any path below the supplied root. There must be no send/reply/move/delete
features and no Gmail, OAuth, IMAP, or SMTP integration. If a later feature
needs an index or cache, put it outside the mail root.

The project uses Go 1.26.1, Bubble Tea v2, Lip Gloss v2, Glamour v2, and an
HTML-to-Markdown converter while still shipping as one native binary. Build and
validate with:

```sh
go test ./...
go vet ./...
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o mailtui .
```

Do not commit the generated binary.

## State of the project

The initial MVP and first UI redesign are complete. It discovers Maildirs
recursively, loads `cur/` and `new/`, parses common RFC 822/MIME mail, shows
important headers and bodies, decodes Base64 and quoted-printable content,
lists attachment metadata, and surfaces invalid messages. Tests live beside
the packages they cover.

Selected messages now preserve plain and HTML alternatives, decode legacy MIME
charsets, render HTML as styled Markdown, and expose bounded true-color previews
for MIME-local PNG, JPEG, and GIF images. `v` toggles rich/plain display. Never
fetch remote HTML image URLs.

The code is separated into `internal/maildir`, `internal/message`, and
`internal/ui`, with the CLI in `main.go`.

Network-mount performance is handled by two-phase asynchronous I/O. Folder
scans concurrently read headers only; full bodies and attachments are loaded
only for the selected message. Both folder and message selection are debounced,
and all I/O runs as Bubble Tea commands rather than inside the event loop. Do
not reintroduce synchronous folder parsing or full-file reads for list rows.
Header scans stream batches of 64 and persist header-only summaries under the
user cache, keyed by a fingerprint of Maildir paths. Attachments are extracted
only after explicit `o`/Enter interaction, sanitized, written outside Maildir,
and opened with the platform's default application.

GitHub Actions run formatting, tests, vetting, and a static build on pushes and
pull requests. Tags matching `v*`, or a manual Actions run with a semantic
version input, build checksummed Linux, macOS, and Windows archives and publish
a GitHub release. `install.sh` installs a selected release on Linux or macOS and
verifies its SHA-256 checksum before copying the binary.

## User feedback and immediate product goal

The initial user feedback was that the functional prototype was ugly and
unfriendly. The first redesign now provides responsive master-detail panes,
styled focus and selection, snippets, automatic preview, folder ranking, and
interactive `/` search. The next UI work should be driven by using this version
against the real backup and polishing concrete issues found there.

Target a responsive master-detail layout:

- folders on the left;
- compact message list/previews in the middle;
- selected email body on the right, or below the list when that fits better;
- borders, spacing, tasteful colors, clear focus/selection states, counts, and
  contextual key hints;
- graceful narrow-terminal fallback.

The selected email should be previewed without requiring an extra Enter. Enter
can focus or expand the reader. Include sender, subject, date/time, and a short
snippet in useful message rows.

Folder order matters: INBOX must always be at the top. Gmail/system folders
(including `[Gmail]` or names beginning with Gmail) should form the next group,
followed by normal user labels in natural case-insensitive order.

Search is urgent. `/` should visibly activate search, filter at least Subject,
From, To, and Cc, show result count, and support Esc cancellation. Keep future
body/SQLite search possible, but never place an index in the Maildir.

## Recommended sequence

1. Evaluate the new layout against a real large backup.
2. Polish spacing, colors, density, and focus behavior from that feedback.
3. Validate empty, malformed, Unicode, attachment, nested-label, long-list, and
   small-terminal cases.

Read `AGENTS.md` for the complete constraints, architecture notes, detailed UX
brief, and definition of done. Treat it as the authoritative contributor guide.
All user-facing text, documentation, comments, fixtures, and commits must remain
in English.
