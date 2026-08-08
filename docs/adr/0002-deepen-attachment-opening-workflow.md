# ADR-0002: Deepen attachment opening into one safety workflow

- Status: Accepted
- Date: 2026-08-08

## Context

The UI currently sequences attachment extraction and platform launching. That
leaks the order of security-sensitive work and makes partial success part of
the UI protocol. The attachment module already contains the meaningful depth:
MIME decoding, filename sanitization, external cache placement, private file
permissions, and atomic replacement.

## Decision

Make attachment opening one deep workflow. The module owns MIME extraction,
deterministic materialization outside the Maildir, platform launch, and the
result policy. Extraction is idempotent for a message path and attachment index
and atomically replaces the same cache path. If launch fails after
materialization, return the path and error and retain the artifact for retry or
manual opening.

Keep OS selection internal. Tests use a deterministic command-runner seam; no
second public adapter is introduced until a real alternative opener exists.
Starting the external application is non-blocking and its later exit status is
not part of the workflow result.

## Consequences

The UI no longer sequences security-sensitive file operations. Cache placement,
permissions, sanitization, and partial failure gain locality and one test
surface. The Maildir remains strictly read-only.
