# mailtui

`mailtui` is a fast, offline, read-only terminal application for browsing email
backups stored in Maildir format.

Point it at the root of an `mbsync`/`isync` backup and it automatically finds
all Maildir folders below it. No Gmail connection, OAuth token, IMAP server, or
SMTP configuration is required.

## Features

- Browse folders, messages, and the selected email in a responsive interface.
- Search the current folder by subject, sender, or recipient.
- Read common RFC 822 and MIME messages, including encoded headers and bodies.
- See attachment names, types, and sizes, then open them with the default app.
- Navigate large backups on local or network-mounted filesystems without
  blocking the interface.
- Keep the original Maildir strictly read-only. mailtui never changes messages,
  flags, folders, or files inside the backup.

## Install

### Linux and macOS

Run the installer:

```sh
curl -fsSL https://raw.githubusercontent.com/vandaimer/mailtui/master/install.sh | sh
```

It downloads the latest binary for your operating system and architecture,
verifies its SHA-256 checksum, and installs it to `~/.local/bin/mailtui`.

If `~/.local/bin` is not already in your `PATH`, add it to your shell:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

To install a particular version or choose another directory:

```sh
curl -fsSL https://raw.githubusercontent.com/vandaimer/mailtui/master/install.sh | \
  sh -s -- --version v0.1.0 --dir "$HOME/bin"
```

### Windows

Download the appropriate `mailtui_windows_*.zip` file from the
[latest release](https://github.com/vandaimer/mailtui/releases/latest), extract
`mailtui.exe`, and place it in a directory included in your `PATH`.

Then open PowerShell or Windows Terminal and run:

```powershell
mailtui.exe 'C:\path\to\mail'
```

### Build from source

Go 1.26.1 or newer is required:

```sh
git clone https://github.com/vandaimer/mailtui.git
cd mailtui
go build -trimpath -o mailtui .
./mailtui /path/to/mail
```

## Open a backup

Pass the root directory containing your Maildir folders:

```sh
mailtui /mnt/mail
```

Other examples:

```sh
mailtui ~/.local/share/mail/mbsync
mailtui '/run/user/1000/gvfs/smb-share:server=nas,share=mail'
```

The expected layout looks like this:

```text
mail/
├── INBOX/
│   ├── cur/
│   ├── new/
│   └── tmp/
├── Receipts/
│   ├── cur/
│   ├── new/
│   └── tmp/
└── [Gmail]/
    └── Sent Mail/
        ├── cur/
        ├── new/
        └── tmp/
```

Pass `mail/`, not an individual message file. Every nested directory containing
`cur/`, `new/`, and `tmp/` is detected as a folder. `INBOX` is shown first,
followed by Gmail system folders and regular labels.

## Controls

| Key | Action |
| --- | --- |
| `↑` / `↓` or `j` / `k` | Navigate the focused pane |
| `Tab` / `Shift+Tab` | Move between folders, messages, and reader |
| `←` / `→` or `h` / `l` | Move between panes |
| `/` | Search subject, sender, and recipients |
| `Enter` | Apply search or move to the next pane |
| `Esc` | Cancel search, clear the filter, or go back |
| `PgUp` / `PgDn` | Scroll the message body |
| `o` | Open the attachment picker |
| `q` | Quit |

The layout adapts to the terminal width. Wide terminals show all three panes;
medium terminals stack the message list and reader; narrow terminals show one
focused pane at a time.

## Attachments

Open a message and press `o`. Select an attachment with `↑`/`↓` or `j`/`k`,
then press `Enter`. PDFs, images, and other files are opened with the default
application configured on your system.

Attachments are decoded only after you explicitly open them. Temporary copies
are stored under:

```text
${XDG_CACHE_HOME:-~/.cache}/mailtui/attachments/
```

The files receive restricted permissions, and nothing is written into the
Maildir backup.

## Large and network-mounted backups

mailtui initially reads message headers only. Full MIME content and attachments
are loaded when a message is selected, while folder headers appear progressively
in small batches.

Reusable header metadata is stored outside the backup under:

```text
${XDG_CACHE_HOME:-~/.cache}/mailtui/metadata-v1/
```

If a folder has not changed, the next visit reuses that cache. Deleting the
cache is safe; mailtui recreates it when needed.

## Current email rendering

mailtui prefers the `text/plain` MIME body. If a message only contains HTML, it
converts that content to basic readable text. Rich HTML formatting and remote
images are not currently rendered.

Unreadable or malformed messages remain visible as invalid entries so you can
inspect the integrity of the backup without interrupting navigation.

## Uninstall

If you used the default installer location:

```sh
rm "$HOME/.local/bin/mailtui"
```

You may also remove the optional cache directory:

```sh
rm -r "${XDG_CACHE_HOME:-$HOME/.cache}/mailtui"
```
