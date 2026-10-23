# Pi transcript reader

## Approved intent

Show Pi conversations in Tuios's existing Inbox instead of the unsupported-reader
message. The user approved an upstream-ready, read-only feature with branch
correctness, implemented on a feature branch from local `main` in an isolated
worktree. Do not install anything, change the Nix configuration, restart the live
daemon, push, or open a pull request as part of implementation.

## Scope

- Decode the tree-structured Pi v2/v3 JSONL session format, checked against Pi
  1.3.0's packaged session-format/message-types documentation and session manager.
- Show user and assistant text, visible thinking, tool calls/results, native edit
  and write diffs, failed tool calls, provider errors and aborted responses.
- Preserve active-branch conversation history, including compaction/branch
  summaries. Model-context edits must not rewrite the displayed raw history.
- Skip system prompt/tool declarations, opaque signatures, extension state and
  hidden custom messages. Render images as placeholders, not base64 payloads.
- Follow live appends, session changes and branch navigation; paginate without
  mixing branches or retaining abandoned entries.
- Use the default `~/.pi/agent/sessions/*/*.jsonl` layout. Custom locations remain
  subject to Tuios's manifest directory allowlist; do not accept arbitrary paths.
- No branch-selection UI, cost dashboard, subagent dashboard or new reply logic.

## Architecture and implementation calls

Keep working/done/question state with the existing hook. Register `pi-jsonl` as
a view-only reader and add a Pi `[transcript]` manifest block. Reuse the existing
Entry schema, rendering, sanitization, path checks and human-only read boundary.

Pi stores a tree using `id` and `parentId`, not a linear conversation. Its
`branch()` can change the in-memory leaf without appending to the file. Extend
the existing report path with optional `transcript_leaf`, and let
`set-agent-session` also accept the path and leaf after its identity checks.
Pi's metadata-only `transcript_update` hook reports use that existing verb. A
present empty leaf means an empty selected branch; an absent leaf means an older
hook and selects the last complete valid entry's ancestry. An invalid explicit
leaf never falls back to another branch.

Keep the selected leaf only in the daemon's transcript join, never persisted or
published in window state. Branch-only updates raise a transcript event even if
file size is unchanged. Serialize the extension's child reports so old leaf or
session reports cannot overtake newer ones. Observe session_start, session_tree,
message persistence and compaction at the verified Pi lifecycle boundaries;
print/RPC/headless children must still never claim the pane.

The reader builds a bounded, request-local projection of the selected ancestry,
then uses the existing paging and Entry finishing code. No background content
decoding or unbounded persistent conversation index. Bound the snapshot by the
existing 32 MiB scan ceiling; refuse larger files rather than silently mixing
branches. Project complete records only, validate tree ancestry/cycles, and keep
stable record/part IDs. Cursors must invalidate when the selected path changes;
ordinary appended descendants must not duplicate older entries. Document this
first-version size limit and old-hook branch-selection fallback.

## Verification and safety

Prefer real-daemon E2E tests as required by AGENTS.md. Use synthetic fixtures,
temporary HOME/XDG directories, and repeatable protocol/screen artifacts. Write
tests before production code and record negative controls. Exercise same-file
branch changes, session replacement, pagination, partial records, malformed
trees, unsafe file paths, secret masking and headless-mode exclusion.

Run affected package tests and targeted E2E tests with nice/taskset, then the
broader short suite and js/wasm compile check. Existing security and other harness
tests must remain green. No new dependencies or library-module pin changes.

## Source references

- Pi: packaged `docs/session-format.md`, `docs/message-types.md`,
  `dist/core/session-manager.js`, `dist/core/extensions/types.d.ts` from 1.3.0.
- Tuios: `internal/transcriptview`, `internal/harness/transcript.go`,
  `internal/session/agent_transcript.go`, `internal/session/verb_handlers.go`,
  `cmd/tuios/agent_hook.go`, and `e2e/tui/cockpit_threads_test.go`.
