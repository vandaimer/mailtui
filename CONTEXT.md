# Domain context

## Message-list projection

The message-list projection is an immutable UI view of the active folder's
messages after folder-local search. It preserves the order supplied by the
read session, resolves path-based selection to a visible position, and owns
safe previous, next, first, and last navigation targets. It does not own
message loading or presentation formatting.

## Reader document

The reader document is the immutable, terminal-width-aware rendering of one
fully hydrated message. It owns header, attachment, body, and local-image
composition; reports the actual rich, plain, or fallback mode; and provides
safe viewport measurements. Its in-memory cache is derived state and never
writes to the Maildir.

## Loaded-folder state

Loaded-folder state is the in-memory lifecycle of one discovered folder after
the read session supplies message snapshots. Folder identity remains immutable
in the Maildir discovery module, where `Folder` has only a path and name;
the synchronous `Load` operation is retired. Loaded-folder state owns
unloaded, loading, loaded, and failed snapshots, progressive replacement,
refresh preservation, and hydration replacement by stable Maildir path. It
performs no filesystem I/O, cache writes, scheduling, sorting, filtering, or
rendering. It preserves the order supplied by the read session and leaves
path-based navigation to the Message-list projection.

## Attachment opening

Attachment opening is the user-triggered workflow that decodes one MIME
attachment, materializes it outside the Maildir under a sanitized deterministic
name with private permissions, and starts the platform default application. It
is idempotent for a message path and attachment index, never mutates the
Maildir, and reports a materialized path separately when launching fails.

## Presentation facts

Presentation facts are the compact, immutable view input assembled by the UI
model from loaded-folder state, interaction state, activity facts, layout, the
Message-list projection, and the Reader document. The presentation module uses
them to render terminal chrome and every visible pane state. It performs no
filesystem I/O, scheduling, cache writes, or state mutation.
