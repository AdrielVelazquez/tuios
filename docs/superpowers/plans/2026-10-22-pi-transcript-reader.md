# Pi Transcript Reader Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Read the active Pi conversation in Tuios's Inbox, including live updates and correct same-file branch selection.

**Architecture:** Add a bounded, view-only Pi JSONL tree projection and decoder to the existing transcript reader. Extend the existing state and identity-only hook reports with optional leaf metadata, held only in the daemon join. Reuse the current rendering, access control, file watching, pagination and sanitization.

**Tech Stack:** Go 1.26, existing Tuios packages and real-daemon Go E2E harness; the existing TypeScript Pi integration asset. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-10-22-pi-transcript-reader-design.md`

## Global Constraints

- Worktree branch `feat/pi-transcript-reader`, based on local `main` at `2bfb9d45a8a7b9a2d93839b53538257978073521`.
- No installs, Nix changes, live daemon restarts, pushes or PR creation.
- View-only reader `pi-jsonl`; Pi v2/v3 trees, checked against Pi 1.3.0.
- Default allowlist `~/.pi/agent/sessions/*/*.jsonl`; no arbitrary-path exception.
- Bound request-local snapshot by the existing 32 MiB scan ceiling.
- Preserve human-only reads, own-pane joins, regular-file checks, secret masking and output bounds.
- Do not change Pi working/done/question semantics, claim headless children, or add dependencies/library pin changes.
- Tests first; prefer real-daemon E2E tests and emit repeatable artifacts.
- Builds and tests use `nice -n 10 taskset -c 0-7` on framework13/Linux.

## Review Focus

- A leaf update arriving before its record is complete must not show a different branch; cover in Task 1/2.
- Same-file tree navigation without a disk append must refresh and remove abandoned messages; cover in Task 2.
- Delayed old-session reports or nested/headless child reports must not replace the visible conversation; cover in Task 2.
- Malformed ancestry, cycles, huge records and binary image bodies must stay bounded and safe; cover in Task 1/3.
- Old hooks/daemons and custom session locations must fail conservatively rather than weaken path/access restrictions; cover in Task 2/3.

## File responsibilities

- New `internal/transcriptview/pi.go`: format identifier, Pi-to-Entry decoding, bounded ancestry projection, branch-aware paging.
- `internal/transcriptview/{format,transcriptview}.go`: decoder registration, optional selected-leaf option and shared read/paging integration.
- `internal/harness/{transcript.go,manifests/pi.toml}`: reader registration and path allowlist.
- `internal/app/inbox_thread.go`: opt Pi into the existing transcript UI.
- `internal/integration/{hook.go,hook_plugins.go,targets.go,assets/pi/tuios-agent-state.ts}`: optional leaf field, metadata-only event, integration version and lifecycle reporting.
- `cmd/tuios/agent_hook.go`: capability-negotiated path/leaf forwarding for state and identity-only reports.
- `internal/session/{agent_transcript.go,verb_agent_transcript.go,verb_handlers.go,verb_protocol.go}`: in-memory selected leaf, safe join updates, refresh events and advertised parameters.
- `internal/session/hosted_calls.go`: prevent remote forwarding of machine-local transcript metadata.
- `internal/transcriptview/testdata/pi-session.jsonl`: synthetic documented format fixture.
- `e2e/tui/{agent_transcript_pi_test.go,cockpit_threads_test.go,NEGATIVE_CONTROLS.md}`: real-daemon reader, lifecycle, security and UI assertions/artifacts.
- Existing hook wire-compatibility tests/fixtures: optional-field negotiation and generated integration behavior.
- `docs/protocol.md` and `skills/tuios/{state,events}.md`: supported reader, leaf parameter and documented limitations; regenerate `skills/tuios.tar.gz` after topic changes.

### Task 1: Read and display a Pi session tree

**Interfaces:** Consume existing `Read(path string, opts Options) (Page, error)`, Entry helpers, file-opening/sanitization and E2E `atRig`. Produce `FormatPi = "pi-jsonl"`, `piLine(r *reader, line []byte, off int64) (decoded, bool)`, and `piRead(path string, opts Options) (Page, error)`. Add `Options.Leaf *string`: nil chooses the newest complete valid entry; pointer to empty chooses no entries; nonempty chooses that exact entry's ancestry.

- [x] **Step 1: Write failing real-daemon tests** `TestPersonReadsPiTranscript` and `TestPiTranscriptTreePaging`, plus the synthetic fixture and a Pi case in `TestCockpitThreadOtherHarnesses`.
  Assert user text appears once, text/thinking/tool call/results/edit/write diffs and errors render, system/hidden/custom metadata and image bytes do not, and sibling-branch markers are absent. Assert paging joins exactly the selected ancestry with stable IDs, compaction summaries do not delete raw historical messages, and context edits do not rewrite displayed history. Include an incomplete last line and explicit unavailable leaf with no wrong-branch fallback. Store protocol and screen artifacts.
- [x] **Step 2: Run to verify expected failure** with `rtk just e2e 'TestPersonReadsPiTranscript|TestPiTranscriptTreePaging|TestCockpitThreadOtherHarnesses/pi'`; expect unsupported reader/absent Pi conversation assertions, not harness setup errors.
- [x] **Step 3: Implement the reader and wiring** in the files assigned above. Resolve ancestry iteratively through an ID/parent index built from complete lines, with cycle/duplicate/size checks and bounded memory. Reuse Entry finishing and paging rather than implementing a second renderer; preserve stable record/part IDs and branch-aware cursor invalidation. Classify Pi's native `read`, `bash`, `edit` (`oldText`/`newText`) and `write` tools; generic extension tools remain generic. Register the manifest allowlist and Inbox support. Do not interpret private signatures or base64 image bodies as text.
- [x] **Step 4: Run Task 1 tests and affected package tests**; all new assertions and existing other-harness transcript cases must pass. Run `rtk proxy nice -n 10 taskset -c 0-7 go test -short ./internal/transcriptview ./internal/harness ./internal/session ./internal/app -timeout 10m`.
- [x] **Step 5: Commit only Task 1 changes** as `feat: read Pi session transcripts in the Inbox`.

### Task 2: Follow Pi's selected branch and lifecycle without changing state

**Interfaces:** Consume Task 1 `Options.Leaf`. Add `Report.TranscriptLeaf *string` with JSON name `transcript_leaf`; retain nil versus pointer-to-empty. Extend `reportHookSession(client verbCaller, sess, window, harness string, harnessPID int, r integration.Report) (hookReportResult, []string, error)` to negotiate optional path/leaf fields. Extend both existing report verbs with optional `transcript_leaf`; identity-only reports additionally accept `transcript_path`. Keep selected leaf solely in the transcript join and copy it under the transcript mutex when building read options.

- [x] **Step 1: Write failing E2E tests** `TestPiTranscriptFollowsSelectedBranch` and `TestPiTranscriptSessionLifecycle`. Report `transcript_update` with selected leaf, change between sibling leaves without modifying the fixture, and assert an event plus fresh correct page/Inbox reset. Assert null/empty selection, later descendant append, stale/old cursors, pre-persistence leaf reporting, /resume to another file, rejected foreign/nested identity and headless modes. Assert metadata-only updates preserve state, source, completion count and prompt status. Exercise the generated extension with a disposable fake Pi event API and real hook CLI, without invoking a provider.
- [x] **Step 2: Run to verify expected failure** with `rtk just e2e 'TestPiTranscriptFollowsSelectedBranch|TestPiTranscriptSessionLifecycle'`; expect absent metadata/refresh assertions.
- [x] **Step 3: Implement report transport and join updates.** Add optional leaf capability negotiation to state reports and identity-only reports. Translate Pi `transcript_update` as SessionOnly, not as idle/done. Apply existing identity and path/own-pane guards before updating the join; propagate `transcript_refused` in explanations. Reject invalid leaf values and ignore absent metadata from old hooks. Raise a transcript event for same-file leaf changes and invalidate cursors as required; do not expose/persist leaf metadata in WindowState. Drop this metadata on hosted forwarding.
- [x] **Step 4: Update the generated Pi asset and integration version.** Report session/path/leaf metadata on the verified persisted-message and compaction boundaries and on `session_tree`; retain the existing TUI-only guard. Serialize child sends and snapshot event metadata before enqueueing so session/branch transitions cannot reorder. Preserve existing state event mappings and best-effort error behavior. Verify the exact Pi event/persistence ordering from the packaged 1.3.0 source before choosing handlers.
- [x] **Step 5: Run Task 1/2 tests and wire-compatibility tests.** Add assertions to existing hook tests for old-daemon optional-field dropping and the empty leaf; run `rtk proxy nice -n 10 taskset -c 0-7 go test -short ./internal/integration ./cmd/tuios ./internal/session -timeout 10m`.
- [x] **Step 6: Commit only Task 2 changes** as `feat: follow Pi transcript branch selections`.

### Task 3: Harden, document and verify the feature branch

**Interfaces:** Consume the reader and live report path from Tasks 1/2; no new public interface.

- [x] **Step 1: Add failing boundary cases** to the real-daemon Pi tests: outside-root path, symlink escape, FIFO, missing file, no-session run, malformed/cyclic/duplicate ancestry, oversized snapshot/record, hostile escape sequences and secret-like content. Assert non-human callers cannot read, unsafe paths are refused, malformed data cannot leak another branch, and bounds/refusals are explicit. Use synthetic data only.
- [x] **Step 2: Run boundary cases and implement only required corrections.** Verify existing `TestTranscriptNonceScope`, `TestTranscriptPathRules`, `TestTranscriptMasksMultiLineSecrets`, `TestPersonPagesBackThroughATranscript` and other-harness cases remain green.
- [x] **Step 3: Record negative controls.** Build disposable variants disabling Pi registration, ancestry filtering, or same-file branch refresh; verify the corresponding new E2E test fails for the intended assertion. Record exact commands/results in `e2e/tui/NEGATIVE_CONTROLS.md`; restore only changes made for those controls.
- [x] **Step 4: Update user/protocol documentation.** Describe supported Pi fields, installation/reload through the existing integration, the default root restriction, 32 MiB ceiling, old-hook last-entry fallback and human-only access. Do not claim new interactive replies, arbitrary custom roots or real-provider live testing.
- [x] **Step 5: Verify broadly** with `rtk just e2e 'Test.*PiTranscript|TestPiTranscript.*|TestCockpitThreadOtherHarnesses|TestTranscriptNonceScope|TestTranscriptPathRules|TestTranscriptMasksMultiLineSecrets|TestPersonPagesBackThroughATranscript'`, `rtk proxy nice -n 10 taskset -c 0-7 go test -short ./... -timeout 20m`, and `rtk proxy env GOOS=js GOARCH=wasm nice -n 10 taskset -c 0-7 go build -o /tmp/opencode/tuios-pi-reader.wasm ./cmd/tuios-wasm`. Run formatting/diff checks and inspect the exact changed-file list.
- [x] **Step 6: Review and commit** the final boundary/docs changes as `test: verify Pi transcript lifecycle and safety`; report branch, worktree, evidence, artifacts and any remaining limitations without merging or deploying.

## Plan self-review

- All approved scope items have an owning task; no deployment or new dashboard work is included.
- Tests precede production changes; E2E is the default and retained unit additions are wire-compatibility only.
- Leaf optionality, empty selection, identity-only transport and request-local decoding are consistent across tasks.
- All five Review Focus classes have explicit assertions in the owning tasks.
- Implementation bodies remain the implementer's responsibility; exact wire names, limits and safety invariants are fixed here.

## Execution notes

- All tasks and the single independent whole-branch review are complete. Its six
  Important findings were reproduced before fixing and verified in one fix pass;
  it reported no Critical or Minor findings. See the additional Pi regression tests
  and `e2e/tui/NEGATIVE_CONTROLS.md` for their repeatable assertions.
- Explicit-leaf wire tests ran in Task 2, after that endpoint existed, rather than
  Task 1. The combined Pi E2E coverage includes default and explicit selection.
- Use test-local `GOFLAGS=-buildvcs=false` on this validation host; its Go VCS
  discovery otherwise incorrectly queries the home directory. Generated-extension
  tests ran with the already-installed Node 24 dependency, not skipped tests.
- The browser compile uses `cmd/tuios-wasm/prepare.sh`, `GOWORK=off` and its scratch
  modfile, as the repository's browser build does; the bare command above omits
  required upstream-dependency stubs. No tracked dependency pins changed.
- The actual packaged Pi 1.3.0 ExtensionRunner and SessionManager also passed a
  synthetic post-persistence/idle-custom-message integration probe. No provider,
  credentials or real conversation was used.
- First-release path/size limits and no new interactive reply/branch UI remain
  intentional. Live-provider behavior is not claimed. Nothing was deployed.
