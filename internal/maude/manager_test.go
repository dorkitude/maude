package maude

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dorkitude/maude/internal/config"
	"github.com/dorkitude/maude/internal/state"
)

type fakeTmux struct {
	has      bool
	captures []string
	calls    []string
}

func (f *fakeTmux) HasSession(context.Context, string) (bool, error) {
	f.calls = append(f.calls, "has")
	return f.has, nil
}

func (f *fakeTmux) NewSession(_ context.Context, session string, cwd string, command []string) error {
	f.calls = append(f.calls, "new:"+session+":"+cwd+":"+strings.Join(command, " "))
	f.has = true
	return nil
}

func (f *fakeTmux) SendKeys(_ context.Context, _ string, keys ...string) error {
	f.calls = append(f.calls, append([]string{"keys"}, keys...)...)
	return nil
}

func (f *fakeTmux) PasteText(_ context.Context, _ string, text string) error {
	f.calls = append(f.calls, "paste:"+text)
	return nil
}

func (f *fakeTmux) CapturePane(context.Context, string, int) (string, error) {
	f.calls = append(f.calls, "capture")
	if len(f.captures) == 0 {
		return "done", nil
	}
	out := f.captures[0]
	f.captures = f.captures[1:]
	return out, nil
}

func (f *fakeTmux) KillSession(context.Context, string) error {
	f.calls = append(f.calls, "kill")
	return nil
}

func TestRunPrintCreatesSessionAndPastesPrompt(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	ft := &fakeTmux{captures: []string{"answer", "answer"}}
	m := NewManager(cfg, state.New(t.TempDir()), ft)
	m.Sleep = func(time.Duration) {}

	res, err := m.RunPrint(context.Background(), RunOptions{Prompt: "hello", Cwd: "/tmp/project"})
	if err != nil {
		t.Fatalf("RunPrint() error = %v", err)
	}
	if res.Session.TmuxSession != "maude-default" {
		t.Fatalf("tmux session = %q", res.Session.TmuxSession)
	}
	if res.Output != "answer" {
		t.Fatalf("output = %q, want answer", res.Output)
	}
	if !containsCall(ft.calls, "paste:hello") {
		t.Fatalf("calls did not paste prompt: %#v", ft.calls)
	}
}

func TestRunPrintSwitchesResume(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	store := state.New(t.TempDir())
	err := store.SaveSession(state.Session{
		Name:         "default",
		SafeName:     "default",
		TmuxSession:  "maude-default",
		PaneTarget:   "maude-default:0.0",
		ClaudeResume: "old",
	})
	if err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}
	ft := &fakeTmux{has: true}
	m := NewManager(cfg, store, ft)
	m.Sleep = func(time.Duration) {}

	_, err = m.RunPrint(context.Background(), RunOptions{Prompt: "next", Resume: "new", NoWait: true})
	if err != nil {
		t.Fatalf("RunPrint() error = %v", err)
	}
	if !containsCall(ft.calls, "C-c") || !containsCall(ft.calls, "paste:claude --dangerously-skip-permissions --resume new") {
		t.Fatalf("resume switch calls = %#v", ft.calls)
	}
}

func TestRunPrintStartsClaudeWithConfiguredArgs(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	cfg.ClaudeArgs = []string{"--permission-mode", "bypassPermissions"}
	ft := &fakeTmux{}
	m := NewManager(cfg, state.New(t.TempDir()), ft)
	m.Sleep = func(time.Duration) {}

	_, err := m.RunPrint(context.Background(), RunOptions{Prompt: "hello", NoWait: true, Cwd: "/tmp/project"})
	if err != nil {
		t.Fatalf("RunPrint() error = %v", err)
	}
	if !containsCall(ft.calls, "new:maude-default:/tmp/project:claude --permission-mode bypassPermissions") {
		t.Fatalf("new session calls = %#v", ft.calls)
	}
}

func TestWaitForClaudeReadyReturnsWhenMarkerAppears(t *testing.T) {
	t.Parallel()

	cfg := config.Defaults()
	cfg.ReadyMarker = "? for shortcuts"
	cfg.ReadyTimeout = "5s"
	cfg.CaptureLines = 10
	ft := &fakeTmux{captures: []string{
		"welcome to claude code",
		"loading...",
		"claude > _\n? for shortcuts",
	}}
	m := NewManager(cfg, state.New(t.TempDir()), ft)
	m.Sleep = func(time.Duration) {}

	if err := m.waitForClaudeReady(context.Background(), "maude-default:0.0"); err != nil {
		t.Fatalf("waitForClaudeReady() error = %v", err)
	}
	got := 0
	for _, c := range ft.calls {
		if c == "capture" {
			got++
		}
	}
	if got < 3 {
		t.Fatalf("expected at least 3 capture calls, got %d (%#v)", got, ft.calls)
	}
}

func TestWaitForClaudeReadyTimesOut(t *testing.T) {
	t.Parallel()

	cfg := config.Defaults()
	cfg.ReadyMarker = "? for shortcuts"
	cfg.ReadyTimeout = "100ms"
	ft := &fakeTmux{captures: []string{"welcome", "welcome", "welcome"}}
	m := NewManager(cfg, state.New(t.TempDir()), ft)
	m.Sleep = func(time.Duration) {}

	start := time.Now()
	var calls int
	m.Now = func() time.Time {
		calls++
		// First call sets deadline at start+100ms; subsequent deadline checks
		// jump past the deadline so the loop exits with a timeout error.
		if calls > 2 {
			return start.Add(time.Second)
		}
		return start
	}

	err := m.waitForClaudeReady(context.Background(), "maude-default:0.0")
	if err == nil {
		t.Fatal("waitForClaudeReady() error = nil, want timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("waitForClaudeReady() error = %v, want a timeout error", err)
	}
}

func TestRunPrintDetectsIntervention(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	ft := &fakeTmux{captures: []string{"Session expired", "Session expired"}}
	m := NewManager(cfg, state.New(t.TempDir()), ft)
	m.Sleep = func(time.Duration) {}

	res, err := m.RunPrint(context.Background(), RunOptions{Prompt: "hello"})
	if err != nil {
		t.Fatalf("RunPrint() error = %v", err)
	}
	if res.Intervention == "" {
		t.Fatal("Intervention is empty")
	}
}

func testConfig() config.Config {
	cfg := config.Defaults()
	cfg.StartupWait = "0s"
	cfg.WaitTimeout = "0s"
	cfg.WaitPollInterval = "0s"
	cfg.CaptureDelay = "0s"
	cfg.ReadyMarker = "" // existing fakeTmux captures don't include the live marker
	return cfg
}

func containsCall(calls []string, want string) bool {
	for _, call := range calls {
		if call == want {
			return true
		}
	}
	return false
}
