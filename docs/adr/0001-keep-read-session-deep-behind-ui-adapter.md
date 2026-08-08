# ADR-0001: Keep the read session deep behind a UI adapter

- Status: Accepted
- Date: 2026-08-08

## Context

The read session owns Maildir listing, header scanning, progressive batches,
metadata caching, hydration, and stale-generation handling. Bubble Tea still
needs to schedule those reads and translate their results into visible state.
The current `Model.Update` path knows too much of the read session's request
protocol.

## Decision

Keep the read session implementation and its concurrency/cache behavior intact.
Deepen a Bubble Tea read adapter inside `internal/ui` to own debounce, request
generation, progressive continuation, and stale-result translation. The
loaded-folder state owns snapshot semantics; the adapter only schedules reads
and emits UI facts. Preserve progressive delivery rather than blocking until a
folder scan completes.

Keep one deterministic reader seam for adapter tests. Do not add another
abstraction until a real second implementation exists.

## Consequences

`Model.Update` can focus on user-visible state rather than request choreography.
Adapter tests can cover duplicate, stale, and progressive reads without
repeating those invariants across the view model. The read session remains the
single deep implementation of Maildir read policy.
