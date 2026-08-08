# Domain context

## Message-list projection

The message-list projection is an immutable UI view of the active folder's
messages after folder-local search. It preserves the order supplied by the
read session, resolves path-based selection to a visible position, and owns
safe previous, next, first, and last navigation targets. It does not own
message loading or presentation formatting.
