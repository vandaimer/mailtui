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

The project uses Go 1.26.1 and Bubble Tea so it can ship as one static native
binary. Build and validate with:

```sh
go test ./...
go vet ./...
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o mailtui .
```

Do not commit the generated binary.

## State of the project

The initial MVP is complete and functional. It discovers Maildirs recursively,
loads `cur/` and `new/`, parses common RFC 822/MIME mail, shows important
headers and bodies, falls back from HTML to basic text, decodes Base64 and
quoted-printable content, lists attachment metadata, and surfaces invalid
messages. Tests are in `main_test.go`.

The current implementation is a prototype concentrated in `main.go`. Before
large UI additions, separate Maildir discovery, message parsing, and UI code
into cohesive packages while keeping behavior and tests intact.

## User feedback and immediate product goal

The current UI works, but it is ugly and unfriendly. Large folders become a
giant plain list, there is no search, and opening a message replaces the list,
which loses context. The next milestone is explicitly a visual and interaction
redesign.

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

1. Refactor the monolith with no behavior regression.
2. Add and test semantic folder ranking.
3. Build the responsive pane layout and style system.
4. Add automatic selected-message preview and snippets.
5. Add `/` search and its input/results states.
6. Validate empty, malformed, Unicode, attachment, nested-label, long-list, and
   small-terminal cases.

Read `AGENTS.md` for the complete constraints, architecture notes, detailed UX
brief, and definition of done. Treat it as the authoritative contributor guide.

