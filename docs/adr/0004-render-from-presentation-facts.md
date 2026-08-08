# ADR-0004: Render terminal panes from presentation facts

- Status: Accepted
- Date: 2026-08-08

## Context

The UI model currently combines asynchronous orchestration with terminal
chrome, folder/message/reader panes, attachment-picker states, and formatting
helpers. Rendering methods read the whole model, so visual changes depend on
unrelated read and interaction state.

## Decision

Keep the UI model as the composition root: it assembles compact immutable
presentation facts from loaded-folder state, interaction state, activity facts,
layout, Message-list projection, and Reader document. A pure presentation
module renders header, footer, three panes, attachment picker, focus, and every
loading, empty, search, error, and activity state.

Layout calculation, Message-list projection, and Reader document remain their
own deep modules. Presentation performs no filesystem I/O, scheduling, cache
writes, process launching, or state mutation.

## Consequences

Presentation tests can be table-driven over visible facts, while a smaller
number of model tests cover composition. The responsive layout, message
navigation, and document rendering modules retain their existing ownership.
