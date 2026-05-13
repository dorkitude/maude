package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafeName(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":                 "default",
		"work":             "work",
		"my session/name":  "my-session-name",
		"../bad session//": "bad-session",
		"!!!":              "default",
	}
	for in, want := range cases {
		if got := SafeName(in); got != want {
			t.Fatalf("SafeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSaveLoadDeleteSession(t *testing.T) {
	t.Parallel()

	store := New(t.TempDir())
	sess := Session{
		Name:        "my session",
		TmuxSession: "maude-my-session",
		PaneTarget:  "maude-my-session:0.0",
		LastStatus:  "ok",
	}
	if err := store.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(store.Root, "sessions", "my-session.json")); err != nil {
		t.Fatalf("session file was not written: %v", err)
	}

	got, err := store.LoadSession("my session")
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if got.Name != sess.Name || got.SafeName != "my-session" || got.TmuxSession != sess.TmuxSession {
		t.Fatalf("LoadSession() = %#v", got)
	}

	list, err := store.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(list) != 1 || list[0].Name != sess.Name {
		t.Fatalf("ListSessions() = %#v", list)
	}

	if err := store.DeleteSession("my session"); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}
	if _, err := store.LoadSession("my session"); !os.IsNotExist(err) {
		t.Fatalf("LoadSession() after delete error = %v, want not exist", err)
	}
}
