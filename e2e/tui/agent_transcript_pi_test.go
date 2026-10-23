package tuie2e

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Pi's file is a tree. Reading it as a linear log leaks the abandoned branch;
// failing to register its reader leaves the person's Inbox empty. These tests
// exercise the actual hook, daemon, human-only read and paging protocol.
func piFixture(t *testing.T, r *atRig) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "internal", "transcriptview", "testdata", "pi-session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(xdgDir(r.base, "HOME"), ".pi", "agent", "sessions", "--work-project--", "2026-10-22_pi-fixture.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func piHook(t *testing.T, r *atRig, payload map[string]any) map[string]any {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(tuiosBin, "agent-hook", "pi", "--session", transcriptSession, "--window", r.wins[0], "--explain")
	cmd.Dir = workDirIn(t, r.base)
	cmd.Env = append(os.Environ(), "SHELL=/bin/sh")
	for _, k := range xdgKeys {
		cmd.Env = append(cmd.Env, k+"="+xdgDir(r.base, k))
	}
	cmd.Stdin = bytes.NewReader(b)
	out, err := cmd.CombinedOutput()
	r.art.add("pi hook", string(out))
	if err != nil {
		t.Fatalf("Pi hook: %v\n%s", err, out)
	}
	var explanation map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(out), &explanation); err != nil {
		t.Fatalf("Pi hook explanation: %v\n%s", err, out)
	}
	return explanation
}

func TestPersonReadsPiTranscript(t *testing.T) {
	r := newATRig(t, "pi-transcript.txt", 1)
	path := piFixture(t, r)
	piHook(t, r, map[string]any{"hook_event_name": "session_start", "session_id": "pi-fixture", "transcript_path": path})
	res := r.read(r.wins[0], nil)
	entries := atEntries(t, res)
	encoded, _ := json.Marshal(entries)
	for _, forbidden := range []string{"ABANDONED-BRANCH", "PRIVATE-", "MODEL-ONLY-REPLACEMENT"} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatalf("Pi transcript exposed %q: %s", forbidden, encoded)
		}
	}
	for _, text := range []string{"Set b to three.", "[image]", "The value lives in app.ts.", "The edit is complete; tests failed.", "The turn stopped: interrupted", "Rate limit reached.", "b is three. One test fails."} {
		if !bytes.Contains(encoded, []byte(text)) {
			t.Errorf("Pi transcript is missing %q: %s", text, encoded)
		}
	}
	if strings.Count(string(encoded), "Set b to three.") != 1 {
		t.Errorf("the prompt was duplicated: %s", encoded)
	}
	var edited, written, failed bool
	for _, e := range entries {
		if e.Kind == "tool_call" && e.Tool == "edit" {
			edited = e.Status == "ok" && e.Diff != nil && e.Diff.File == "/work/project/app.ts" && e.Diff.Added == 1 && e.Diff.Removed == 1
		}
		if e.Kind == "tool_call" && e.Tool == "write" {
			written = e.Status == "ok" && e.Diff != nil && e.Diff.Added == 1
		}
		if e.Kind == "tool_call" && e.Tool == "bash" {
			failed = e.Status == "error" && e.Target == "npm test"
		}
	}
	if !edited || !written || !failed {
		t.Fatalf("Pi tool cards: edit=%v write=%v failed=%v\n%s", edited, written, failed, encoded)
	}
}

func TestPiTranscriptTreePaging(t *testing.T) {
	r := newATRig(t, "pi-transcript-paging.txt", 1)
	path := piFixture(t, r)
	// An incomplete sibling is not the selected leaf until its line is whole.
	appendRaw(t, path, `{"type":"message","id":"partial","parentId":"u0","message":`)
	piHook(t, r, map[string]any{"hook_event_name": "session_start", "session_id": "pi-fixture", "transcript_path": path})
	whole := r.read(r.wins[0], nil)
	var want, got []string
	for _, e := range atEntries(t, whole) {
		want = append(want, e.ID)
	}
	page := r.read(r.wins[0], map[string]any{"limit": 2})
	for n := 0; ; n++ {
		if n > 30 {
			t.Fatal("Pi paging did not terminate")
		}
		var ids []string
		for _, e := range atEntries(t, page) {
			ids = append(ids, e.ID)
		}
		got = append(ids, got...)
		older, _ := page["older"].(string)
		if older == "" {
			break
		}
		page = r.read(r.wins[0], map[string]any{"limit": 2, "before": older})
	}
	if len(want) == 0 || !slices.Equal(got, want) {
		t.Fatalf("Pi paged IDs %v, want exactly %v", got, want)
	}
	end := whole["cursor"].(string)
	page = r.read(r.wins[0], map[string]any{"after": end})
	if page["reset"] != false || len(atEntries(t, page)) != 0 {
		t.Fatalf("Pi cursor consumed a partial line or duplicated entries: %v", page)
	}
}
