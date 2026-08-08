# ADR-0003: Separate loaded-folder state from Maildir discovery

- Status: Accepted
- Date: 2026-08-08

## Context

`maildir.Folder` currently contains both discovered identity and mutable
message snapshots. Its synchronous `Load` operation scans headers and writes
those snapshots directly into the discovery value. Production browsing already
uses the read session for cached, progressive, asynchronous reads, so this
leaves two ownership paths for the same state.

## Decision

Reduce `maildir.Folder` to immutable path and name identity and retire
`maildir.Load`. Create pure in-memory loaded-folder state that consumes read
facts and owns unloaded, loading, loaded, and failed snapshots; progressive
replacement; last-good refresh preservation; and hydration replacement by
stable Maildir path.

Loaded-folder state preserves the order supplied by the read session. It does
not perform I/O, caching, scheduling, sorting, filtering, rendering, or
navigation. Message-list projection retains path-based filtering and navigation
ownership.

## Consequences

There is one source of truth for active message snapshots. Tests can exercise
folder transitions without Bubble Tea. The legacy discovery/load test splits
into discovery coverage and loaded-folder/read-session coverage. Refresh
failures retain the last good snapshot, while initial failures remain terminal
without a snapshot.
