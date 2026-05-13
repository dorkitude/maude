package maude

import (
	"context"
	"reflect"
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
	f.calls = append(f.calls, "new:"+session+":"+cwd+":"+reflect.ValueOf(command).String())
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
	if !containsCall(ft.calls, "C-c") || !containsCall(ft.calls, "paste:claude --resume new") {
		t.Fatalf("resume switch calls = %#v", ft.calls)
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
