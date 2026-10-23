package tuie2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Gaurav-Gosain/tuitest"
)

func piUpdate(t *testing.T, r *atRig, path, session, leaf string) {
	t.Helper()
	out := piHook(t, r, map[string]any{"hook_event_name": "transcript_update", "session_id": session, "transcript_path": path, "transcript_leaf": leaf})
	if out["applied"] != true || out["transcript_refused"] != nil {
		t.Fatalf("Pi metadata update was not accepted: %v", out)
	}
}

func piState(t *testing.T, r *atRig) map[string]any {
	t.Helper()
	res, err := r.ctl.call(t, "get-agent-state", map[string]any{"session": transcriptSession, "window": r.wins[0]})
	if err != nil {
		t.Fatal(err)
	}
	state := make(map[string]any)
	for _, key := range []string{"state", "source", "agent_state_at", "blocked_by", "message"} {
		if _, present := res[key]; !present {
			t.Fatalf("agent state omitted %q", key)
		}
		state[key] = res[key]
	}
	// Completion accounting is exposed by list-agents, not get-agent-state.
	agents, err := r.ctl.call(t, "list-agents", map[string]any{"session": transcriptSession})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range agents["agents"].([]any) {
		a := item.(map[string]any)
		if a["window_id"] == r.wins[0] {
			if _, present := a["completion_seq"]; !present {
				t.Fatal("agent list omitted completion_seq")
			}
			state["completion_seq"] = a["completion_seq"]
			return state
		}
	}
	t.Fatal("Pi pane was absent from list-agents")
	return state
}

func piWaitText(t *testing.T, r *atRig, want, absent string) map[string]any {
	t.Helper()
	until := time.Now().Add(uiTimeout)
	for {
		res, err := r.ctl.call(t, "agent-transcript", map[string]any{"session": transcriptSession, "window": r.wins[0], "human_nonce": r.nonce})
		b, _ := json.Marshal(res["entries"])
		if err == nil && strings.Contains(string(b), want) && (absent == "" || !strings.Contains(string(b), absent)) {
			r.art.call("agent-transcript", nil, res, nil)
			return res
		}
		if time.Now().After(until) {
			t.Fatalf("Pi conversation never showed %q without %q: %s %v", want, absent, b, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func piTranscriptEvent(t *testing.T, r *atRig, sub *linkStream) {
	t.Helper()
	sub.timeout = uiTimeout
	defer func() { sub.timeout = 0 }()
	for {
		line, err := sub.br.ReadString('\n')
		if err != nil {
			t.Fatalf("no Pi transcript event: %v", err)
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		r.art.add("Pi transcript event", line)
		if event["type"] == "transcript" && event["window"] == r.wins[0] {
			for key := range event {
				switch key {
				case "type", "session", "window", "cursor", "seq", "boot_id", "time":
				default:
					t.Fatalf("Pi transcript event exposed private metadata %q", key)
				}
			}
			return
		}
	}
}

// Removing the identity-only leaf transport or the branch-only event leaves
// this Inbox on a3 although Pi selected old, without changing a byte on disk.
func TestPiTranscriptFollowsSelectedBranch(t *testing.T) {
	r := newATRig(t, "pi-selected-branch.txt", 1)
	path := piFixture(t, r)
	piHook(t, r, map[string]any{"hook_event_name": "session_start", "session_id": "pi-fixture", "transcript_path": path, "transcript_leaf": "a3", "busy": true})
	first := r.read(r.wins[0], nil)
	before := piState(t, r)
	stat, _ := os.Stat(path)
	term := startIn(t, r.base, startOpts{args: []string{"attach", transcriptSession}, cols: 160, rows: 50, shippedLooks: true})
	if err := term.WaitFor(func(s tuitest.Screen) bool { return countWindows(s) == 1 }, bootTimeout); err != nil {
		t.Fatal(err)
	}
	windowManagementMode(t, term)
	if err := term.SendKeys(tuitest.Ctrl('b'), "i"); err != nil {
		t.Fatal(err)
	}
	if err := term.WaitFor(func(s tuitest.Screen) bool { return strings.Contains(s.Text(), "b is three.") }, uiTimeout); err != nil {
		t.Fatalf("initial Pi Inbox: %v\n%s", err, term.Snapshot())
	}
	sub := r.link.open(t, true)
	if _, err := sub.call(t, "subscribe", map[string]any{"session": transcriptSession, "types": []string{"transcript"}}); err != nil {
		t.Fatal(err)
	}
	piUpdate(t, r, path, "pi-fixture", "old")
	piTranscriptEvent(t, r, sub)
	res := r.read(r.wins[0], map[string]any{"after": first["cursor"]})
	b, _ := json.Marshal(res["entries"])
	if res["reset"] != true || !strings.Contains(string(b), "ABANDONED-BRANCH") || strings.Contains(string(b), "b is three.") {
		t.Fatalf("same-file branch selection did not reset the conversation: %v", res)
	}
	if err := term.WaitFor(func(s tuitest.Screen) bool {
		return strings.Contains(s.Text(), "ABANDONED-BRANCH") && !strings.Contains(s.Text(), "b is three.")
	}, uiTimeout); err != nil {
		t.Fatalf("Pi Inbox kept the previous branch: %v\n%s", err, term.Snapshot())
	}
	saveCockpitFrames(t, term, "pi-selected-branch")
	after, _ := os.Stat(path)
	if stat.Size() != after.Size() || !stat.ModTime().Equal(after.ModTime()) {
		t.Fatal("the branch-selection test changed the session file")
	}
	if !reflect.DeepEqual(before, piState(t, r)) {
		t.Fatal("metadata-only selection changed agent state, source, prompt or completion count")
	}
	// Rewinding before every root must clear the old cards, not select the
	// most recently appended branch by treating the empty string as absent.
	piUpdate(t, r, path, "pi-fixture", "")
	if got := r.read(r.wins[0], map[string]any{"after": res["cursor"]}); got["reset"] != true || len(atEntries(t, got)) != 0 {
		t.Fatalf("empty selected branch retained messages: %v", got)
	}
	piUpdate(t, r, path, "pi-fixture", "old")
	old := r.read(r.wins[0], nil)
	appendLines(t, path, `{"type":"message","id":"child","parentId":"old","timestamp":"2026-10-22T11:00:00Z","message":{"role":"assistant","content":[{"type":"text","text":"DESCENDANT-APPEND"}],"stopReason":"stop"}}`)
	piUpdate(t, r, path, "pi-fixture", "child")
	got := r.read(r.wins[0], map[string]any{"after": old["cursor"]})
	entries := atEntries(t, got)
	if got["reset"] != false || len(entries) != 1 || entries[0].Text != "DESCENDANT-APPEND" {
		t.Fatalf("a descendant append duplicated/reset the conversation: %v", got)
	}
	piUpdate(t, r, path, "pi-fixture", "pending")
	pending := r.read(r.wins[0], map[string]any{"after": got["cursor"]})
	if pending["reset"] != true || len(atEntries(t, pending)) != 0 {
		t.Fatalf("a leaf reported before persistence exposed another branch: %v", pending)
	}
	record := `{"type":"message","id":"pending","parentId":"child","timestamp":"2026-10-22T11:00:02Z","message":{"role":"assistant","content":[{"type":"text","text":"PENDING-NOW-COMPLETE"}],"stopReason":"stop"}}` + "\n"
	appendRaw(t, path, record[:30])
	if got := r.read(r.wins[0], nil); len(atEntries(t, got)) != 0 {
		t.Fatalf("an incomplete selected leaf exposed another branch: %v", got)
	}
	appendRaw(t, path, record[30:])
	piWaitText(t, r, "PENDING-NOW-COMPLETE", "b is three.")
}

// The installed generated extension drives the real hook and daemon. The fake
// Pi API supplies only lifecycle events, never a fake Tuios reply or decoder.
func TestPiTranscriptSessionLifecycle(t *testing.T) {
	r := newATRig(t, "pi-transcript-lifecycle.txt", 1)
	path := piFixture(t, r)
	d := startPlugin(t, r.base, "pi", transcriptSession, r.wins[0])
	emit := func(name, file, session string, leaf any, mode string) {
		d.emit(t, map[string]any{"type": name, "sessionFile": file, "sessionId": session, "leaf": leaf, "mode": mode, "idle": true})
	}
	emit("session_start", path, "pi-fixture", "a3", "tui")
	piWaitText(t, r, "b is three.", "ABANDONED-BRANCH")
	emit("session_tree", path, "pi-fixture", "old", "tui")
	piWaitText(t, r, "ABANDONED-BRANCH", "b is three.")
	// Pi dispatches message_end before persistence. The driver changes the
	// leaf after the handler returns, as AgentSession does after appending.
	record := map[string]any{"type": "message", "id": "late", "parentId": "old", "timestamp": "2026-10-22T11:00:01Z", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "PERSISTED-MESSAGE"}}, "stopReason": "stop"}}
	d.emit(t, map[string]any{"type": "message_end", "sessionFile": path, "sessionId": "pi-fixture", "leaf": "old", "persistLeaf": "late", "persistRecord": record, "mode": "tui"})
	piWaitText(t, r, "PERSISTED-MESSAGE", "b is three.")
	// A resume changes file and session together; late old-session metadata
	// may never take it back, including when the new session is at rest.
	other := filepath.Join(filepath.Dir(path), "2026-10-22_second.jsonl")
	text := `{"type":"session","version":3,"id":"second","timestamp":"2026-10-22T12:00:00Z","cwd":"/work/project"}` + "\n" +
		`{"type":"message","id":"second1","parentId":null,"timestamp":"2026-10-22T12:00:01Z","message":{"role":"user","content":"RESUMED-SESSION"}}` + "\n"
	if err := os.WriteFile(other, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	emit("session_start", other, "second", "second1", "tui")
	piWaitText(t, r, "RESUMED-SESSION", "PERSISTED-MESSAGE")
	piHook(t, r, map[string]any{"hook_event_name": "transcript_update", "session_id": "pi-fixture", "transcript_path": path, "transcript_leaf": "a3"})
	piWaitText(t, r, "RESUMED-SESSION", "b is three.")
	for _, mode := range []string{"rpc", "print", "json"} {
		emit("session_start", path, "headless", "old", mode)
		emit("session_tree", path, "headless", "old", mode)
	}
	// session_shutdown waits for pending child reports. Its acknowledgement
	// fences the ignored events before we inspect, rather than masking a
	// headless takeover with another successful TUI session_start.
	d.emit(t, map[string]any{"type": "session_shutdown", "ack": "headless-drained"})
	select {
	case ack := <-d.acks:
		if ack != "headless-drained" {
			t.Fatalf("unexpected driver acknowledgement %q", ack)
		}
	case <-time.After(uiTimeout):
		t.Fatal("Pi integration did not drain pending reports")
	}
	res := r.read(r.wins[0], nil)
	b, _ := json.Marshal(res["entries"])
	if !strings.Contains(string(b), "RESUMED-SESSION") || strings.Contains(string(b), "ABANDONED-BRANCH") {
		t.Fatalf("a headless child replaced the pane's transcript: %s", b)
	}
	emit("session_start", other, "second", "second1", "tui")
	piWaitText(t, r, "RESUMED-SESSION", "ABANDONED-BRANCH")
}
