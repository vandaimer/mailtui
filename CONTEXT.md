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
