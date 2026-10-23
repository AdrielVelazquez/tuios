package tuie2e

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Pi must use the same path/human boundary as every other reader, despite
// having its own request-local tree projection rather than the linear reader.
func TestPiTranscriptSafety(t *testing.T) {
	r := newATRig(t, "pi-transcript-safety.txt", 1)
	path := piFixture(t, r)
	piHook(t, r, map[string]any{"hook_event_name": "session_start", "session_id": "pi-fixture", "transcript_path": path})
	initial := r.read(r.wins[0], nil)
	if len(atEntries(t, initial)) == 0 {
		t.Fatal("positive read was empty")
	}
	if _, err := r.ctl.call(t, "agent-transcript", map[string]any{"session": transcriptSession, "window": r.wins[0]}); err == nil || err.Code != "not_human" {
		t.Fatalf("non-human Pi transcript read: %v", err)
	}
	outside := filepath.Join(r.base, "outside.jsonl")
	if err := os.WriteFile(outside, []byte("OUTSIDE-SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(filepath.Dir(path), "escape.jsonl")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(filepath.Dir(path), "pipe.jsonl")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, unsafe := range []string{outside, link, fifo, filepath.Join(filepath.Dir(path), "notes.txt")} {
		out := piHook(t, r, map[string]any{"hook_event_name": "transcript_update", "session_id": "pi-fixture", "transcript_path": unsafe, "transcript_leaf": "a3"})
		if why, _ := out["transcript_refused"].(string); why == "" {
			t.Fatalf("unsafe Pi path accepted: %v", out)
		}
		if got := r.read(r.wins[0], nil); got["cursor"] != initial["cursor"] {
			t.Fatalf("refused path replaced the join: %v", got)
		}
	}
	// Raw metadata is bounded before either verb can change the agent state.
	for _, leaf := range []any{strings.Repeat("x", 129), "../secret", "bad\x1bid", 42} {
		_, err := r.ctl.call(t, "set-agent-session", map[string]any{"session": transcriptSession, "window": r.wins[0], "harness": "pi", "agent_session_id": "pi-fixture", "transcript_path": path, "transcript_leaf": leaf})
		if err == nil || err.Code != "invalid_params" {
			t.Fatalf("invalid Pi leaf was not refused: %v", err)
		}
	}
	// Shared finishing strips escape/bidi controls and masks tool arguments.
	message := map[string]any{"type": "message", "id": "hostile", "parentId": "a3", "timestamp": "2026-10-22T12:00:00Z", "message": map[string]any{"role": "assistant", "stopReason": "toolUse", "content": []any{
		map[string]any{"type": "text", "text": "\x1b[31mSAFE-COLOR\x1b[0m\u202e"},
		map[string]any{"type": "toolCall", "id": "secret-tool", "name": "bash", "arguments": map[string]any{"command": "API_TOKEN=sk-live-pi-boundary-SECRET go test ./..."}},
	}}}
	b, _ := json.Marshal(message)
	appendLines(t, path, string(b))
	got := r.read(r.wins[0], nil)
	encoded, _ := json.Marshal(got["entries"])
	if bytes.Contains(encoded, []byte("sk-live-pi-boundary-SECRET")) || !bytes.Contains(encoded, []byte("[redacted]")) {
		t.Fatalf("Pi tool secret was not masked: %s", encoded)
	}
	for _, e := range atEntries(t, got) {
		if strings.ContainsAny(e.Text, "\x1b\u202e") {
			t.Fatalf("Pi text kept terminal/bidi controls: %q", e.Text)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	_, err := r.ctl.call(t, "agent-transcript", map[string]any{"session": transcriptSession, "window": r.wins[0], "human_nonce": r.nonce})
	if err == nil || err.Code != "no_transcript" {
		t.Fatalf("gone Pi file: %v", err)
	}
}

// Incorrect ancestry must be refused, never silently replaced by another
// branch. Bounds are checked before decoding or opening a non-regular file.
func TestPiTranscriptMalformedAndBounded(t *testing.T) {
	r := newATRig(t, "pi-transcript-bounds.txt", 1)
	path := piFixture(t, r)
	header := `{"type":"session","version":3,"id":"pi-fixture","timestamp":"2026-10-22T12:00:00Z","cwd":"/work/project"}` + "\n"
	for _, tc := range []struct{ name, records string }{
		{"cycle", `{"type":"message","id":"a","parentId":"b","message":{"role":"user","content":"CYCLE-SECRET"}}` + "\n" + `{"type":"message","id":"b","parentId":"a","message":{"role":"user","content":"CYCLE-SECRET"}}` + "\n"},
		{"missing parent", `{"type":"message","id":"a","parentId":"missing","message":{"role":"user","content":"ORPHAN-SECRET"}}` + "\n"},
		{"duplicate id", `{"type":"message","id":"a","parentId":null,"message":{"role":"user","content":"DUPLICATE-SECRET"}}` + "\n" + `{"type":"message","id":"a","parentId":null,"message":{"role":"user","content":"DUPLICATE-SECRET"}}` + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(header+tc.records), 0o600); err != nil {
				t.Fatal(err)
			}
			piHook(t, r, map[string]any{"hook_event_name": "session_start", "session_id": "pi-fixture", "transcript_path": path})
			res, err := r.ctl.call(t, "agent-transcript", map[string]any{"session": transcriptSession, "window": r.wins[0], "human_nonce": r.nonce})
			r.art.call("agent-transcript", nil, res, err)
			if err == nil || err.Code != "internal" || !strings.Contains(err.Message, "Pi") {
				t.Fatalf("invalid Pi ancestry needs a safe, explicit diagnostic: %v %v", res, err)
			}
		})
	}
	for _, size := range []int{16<<20 + 1, 32<<20 + 1} {
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if size < 32<<20 {
			_, err = f.WriteString(header + strings.Repeat("x", size) + "\n")
		} else {
			err = f.Truncate(int64(size))
		}
		_ = f.Close()
		if err != nil {
			t.Fatal(err)
		}
		piHook(t, r, map[string]any{"hook_event_name": "session_start", "session_id": "pi-fixture", "transcript_path": path})
		_, verr := r.ctl.call(t, "agent-transcript", map[string]any{"session": transcriptSession, "window": r.wins[0], "human_nonce": r.nonce})
		if verr == nil || verr.Code != "no_transcript" || !strings.Contains(verr.Message, "too large") {
			t.Fatalf("oversized Pi transcript (%d): %v", size, verr)
		}
	}
}

func TestPiTranscriptEphemeralDoesNotReuseHistory(t *testing.T) {
	r := newATRig(t, "pi-transcript-ephemeral.txt", 1)
	path := piFixture(t, r)
	d := startPlugin(t, r.base, "pi", transcriptSession, r.wins[0])
	d.emit(t, map[string]any{"type": "session_start", "sessionFile": path, "sessionId": "pi-fixture", "leaf": "a3", "mode": "tui"})
	piWaitText(t, r, "b is three.", "ABANDONED-BRANCH")
	d.emit(t, map[string]any{"type": "session_start", "sessionFile": "", "sessionId": "ephemeral", "leaf": nil, "mode": "tui"})
	d.emit(t, map[string]any{"type": "session_shutdown", "ack": "ephemeral-drained"})
	select {
	case <-d.acks:
	case <-time.After(uiTimeout):
		t.Fatal("ephemeral session report did not drain")
	}
	res, err := r.ctl.call(t, "agent-transcript", map[string]any{"session": transcriptSession, "window": r.wins[0], "human_nonce": r.nonce})
	r.art.call("agent-transcript", nil, res, err)
	if err == nil || err.Code != "no_transcript" {
		t.Fatalf("ephemeral Pi session reused another conversation's history: %v %v", res, err)
	}
}
