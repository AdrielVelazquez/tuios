package tuie2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Gaurav-Gosain/tuitest"
)

func TestPiTranscriptRefusedReplacementClearsHistory(t *testing.T) {
	r := newATRig(t, "pi-refused-replacement.txt", 1)
	path := piFixture(t, r)
	piHook(t, r, map[string]any{"hook_event_name": "session_start", "session_id": "pi-fixture", "transcript_path": path, "transcript_leaf": "a3"})
	if len(atEntries(t, r.read(r.wins[0], nil))) == 0 {
		t.Fatal("positive read was empty")
	}
	outside := filepath.Join(r.base, "custom-sessions", "second.jsonl")
	if err := os.MkdirAll(filepath.Dir(outside), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte(`{"type":"session","version":3,"id":"second"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := piHook(t, r, map[string]any{"hook_event_name": "session_start", "session_id": "second", "transcript_path": outside, "transcript_leaf": ""})
	if out["applied"] != true || out["transcript_refused"] == nil {
		t.Fatalf("expected an accepted identity and refused path: %v", out)
	}
	res, err := r.ctl.call(t, "agent-transcript", map[string]any{"session": transcriptSession, "window": r.wins[0], "human_nonce": r.nonce})
	r.art.call("agent-transcript", nil, res, err)
	if err == nil || err.Code != "no_transcript" {
		t.Fatalf("refused replacement exposed previous conversation: %v %v", res, err)
	}
}

func TestPiTranscriptLiveToolResultUpdatesCall(t *testing.T) {
	r := newATRig(t, "pi-live-tool-result.txt", 1)
	path := piFixture(t, r)
	piHook(t, r, map[string]any{"hook_event_name": "session_start", "session_id": "pi-fixture", "transcript_path": path, "transcript_leaf": "a1", "busy": true})
	term := startIn(t, r.base, startOpts{args: []string{"attach", transcriptSession}, cols: 160, rows: 50, shippedLooks: true})
	if err := term.WaitFor(func(s tuitest.Screen) bool { return countWindows(s) == 1 }, bootTimeout); err != nil {
		t.Fatal(err)
	}
	windowManagementMode(t, term)
	if err := term.SendKeys(tuitest.Ctrl('b'), "i"); err != nil {
		t.Fatal(err)
	}
	if err := term.WaitFor(func(s tuitest.Screen) bool { return strings.Contains(s.Text(), "npm test") }, uiTimeout); err != nil {
		t.Fatal(err)
	}
	piUpdate(t, r, path, "pi-fixture", "r2")
	if err := term.WaitFor(func(s tuitest.Screen) bool {
		return strings.Contains(s.Text(), "Command failed with exit code 1") && strings.Contains(s.Text(), "failed")
	}, uiTimeout); err != nil {
		t.Fatalf("incremental failure left the retained tool running: %v\n%s", err, term.Snapshot())
	}
	saveCockpitFrames(t, term, "pi-live-tool-result")
}

func TestPiTranscriptIdentityOnlyReplacementClearsHistory(t *testing.T) {
	r := newATRig(t, "pi-identity-only-replacement.txt", 1)
	path := piFixture(t, r)
	piHook(t, r, map[string]any{"hook_event_name": "session_start", "session_id": "pi-fixture", "transcript_path": path, "transcript_leaf": "a3"})
	r.read(r.wins[0], nil)
	out, err := r.ctl.call(t, "set-agent-session", map[string]any{"session": transcriptSession, "window": r.wins[0], "harness": "pi", "agent_session_id": "replacement-without-file"})
	if err != nil || out["applied"] != true {
		t.Fatalf("identity replacement: %v %v", out, err)
	}
	res, err := r.ctl.call(t, "agent-transcript", map[string]any{"session": transcriptSession, "window": r.wins[0], "human_nonce": r.nonce})
	r.art.call("agent-transcript", nil, res, err)
	if err == nil || err.Code != "no_transcript" {
		t.Fatalf("identity-only replacement reused stale history: %v %v", res, err)
	}
}

func TestPiTranscriptWaitsForActualPersistence(t *testing.T) {
	r := newATRig(t, "pi-actual-persistence.txt", 1)
	path := piFixture(t, r)
	d := startPlugin(t, r.base, "pi", transcriptSession, r.wins[0])
	d.emit(t, map[string]any{"type": "session_start", "sessionFile": path, "sessionId": "pi-fixture", "leaf": "a3", "mode": "tui"})
	piWaitText(t, r, "b is three.", "ABANDONED-BRANCH")
	// A subsequent extension can await asynchronous work before Pi persists
	// this message. One setImmediate is NOT a post-persistence boundary.
	d.emit(t, map[string]any{"type": "message_end", "leaf": "a3", "persistLeaf": "afterasync", "persistenceDelay": 80,
		"persistRecord": map[string]any{"type": "message", "id": "afterasync", "parentId": "a3", "timestamp": "2026-10-22T13:00:00Z", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "AFTER-ASYNC-PERSISTENCE"}}, "stopReason": "stop"}}})
	piWaitText(t, r, "AFTER-ASYNC-PERSISTENCE", "ABANDONED-BRANCH")
}

func TestPiTranscriptIdleCustomAppend(t *testing.T) {
	r := newATRig(t, "pi-idle-custom.txt", 1)
	path := piFixture(t, r)
	d := startPlugin(t, r.base, "pi", transcriptSession, r.wins[0])
	d.emit(t, map[string]any{"type": "session_start", "sessionFile": path, "sessionId": "pi-fixture", "leaf": "a3", "mode": "tui"})
	piWaitText(t, r, "b is three.", "ABANDONED-BRANCH")
	// _appendCustomMessage emits public events, not extension message_end.
	// Persisting this visible message must work without any lifecycle handler.
	d.emit(t, map[string]any{"type": "persist_only", "persistLeaf": "idlecustom", "persistRecord": map[string]any{"type": "custom_message", "id": "idlecustom", "parentId": "a3", "display": true, "content": "VISIBLE-IDLE-CUSTOM"}})
	piWaitText(t, r, "VISIBLE-IDLE-CUSTOM", "ABANDONED-BRANCH")
}

func TestPiTranscriptDirectShellHistory(t *testing.T) {
	r := newATRig(t, "pi-direct-shell.txt", 1)
	path := piFixture(t, r)
	parent := "a3"
	for _, tc := range []struct {
		id, command, output  string
		code                 int
		cancelled, truncated bool
	}{
		{"shellok", "echo direct", "DIRECT-SHELL-OK", 0, false, false},
		{"shellfail", "false", "DIRECT-SHELL-FAIL", 1, false, false},
		{"shellcancel", "sleep 99", "", 0, true, true},
	} {
		b, _ := json.Marshal(map[string]any{"type": "message", "id": tc.id, "parentId": parent, "timestamp": "2026-10-22T13:00:00Z", "message": map[string]any{"role": "bashExecution", "command": tc.command, "output": tc.output, "exitCode": tc.code, "cancelled": tc.cancelled, "truncated": tc.truncated, "excludeFromContext": true}})
		appendLines(t, path, string(b))
		parent = tc.id
	}
	piHook(t, r, map[string]any{"hook_event_name": "session_start", "session_id": "pi-fixture", "transcript_path": path, "transcript_leaf": parent})
	entries := atEntries(t, r.read(r.wins[0], nil))
	var commands []string
	for _, e := range entries {
		if e.Kind == "tool_call" && e.Tool == "bash" && e.Target != "npm test" {
			commands = append(commands, e.Target+":"+e.Status)
		}
	}
	if strings.Join(commands, "|") != "echo direct:ok|false:error|sleep 99:error" {
		t.Fatalf("direct shell statuses: %v", commands)
	}
	b, _ := json.Marshal(entries)
	for _, text := range []string{"DIRECT-SHELL-OK", "DIRECT-SHELL-FAIL", "Command cancelled"} {
		if !strings.Contains(string(b), text) {
			t.Fatalf("direct shell history omitted %q: %s", text, b)
		}
	}
}

func TestPiTranscriptReportQueueHasBoundedDrain(t *testing.T) {
	r := newATRig(t, "pi-report-drain.txt", 1)
	path := piFixture(t, r)
	command := filepath.Join(t.TempDir(), "slow-report")
	capture := filepath.Join(t.TempDir(), "reports.jsonl")
	t.Setenv("TUIOS_PI_TEST_CAPTURE", capture)
	if err := os.WriteFile(command, []byte("#!/bin/sh\ncat >> \"$TUIOS_PI_TEST_CAPTURE\"\nprintf '\\n' >> \"$TUIOS_PI_TEST_CAPTURE\"\nsleep 0.3\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	d := startPlugin(t, r.base, "pi", transcriptSession, r.wins[0], command)
	d.emit(t, map[string]any{"type": "session_start", "sessionFile": path, "sessionId": "pi-fixture", "leaf": "a3", "mode": "tui"})
	for range 10 {
		d.emit(t, map[string]any{"type": "session_tree", "leaf": "a3"})
	}
	start := time.Now()
	d.emit(t, map[string]any{"type": "session_shutdown", "ack": "bounded-drain"})
	select {
	case <-d.acks:
	case <-time.After(2 * time.Second):
		t.Fatal("report backlog made Pi shutdown exceed the total drain budget")
	}
	r.art.add("shutdown drain", time.Since(start).String())
	b, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	reports := strings.FieldsFunc(string(b), func(r rune) bool { return r == '\n' })
	if len(reports) > 3 {
		t.Fatalf("superseded metadata was not coalesced: %d reports", len(reports))
	}
}
