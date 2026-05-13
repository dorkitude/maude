package tmux

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type call struct {
	name string
	args []string
}

type fakeExec struct {
	calls []call
	errs  []error
}

func (f *fakeExec) Run(_ context.Context, name string, args ...string) (string, error) {
	f.calls = append(f.calls, call{name: name, args: append([]string(nil), args...)})
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		return "", err
	}
	return "out", nil
}

func TestNewSessionCommand(t *testing.T) {
	t.Parallel()

	exec := &fakeExec{}
	client := Client{Bin: "tmux", Exec: exec}
	err := client.NewSession(context.Background(), "maude-default", "/tmp/project", []string{"claude", "--resume", "abc 123"})
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	want := []string{"new-session", "-d", "-s", "maude-default", "-c", "/tmp/project", "claude --resume 'abc 123'"}
	if !reflect.DeepEqual(exec.calls[0].args, want) {
		t.Fatalf("args = %#v, want %#v", exec.calls[0].args, want)
	}
}

func TestPasteTextUsesBuffer(t *testing.T) {
	t.Parallel()

	exec := &fakeExec{}
	client := Client{Bin: "tmux", Exec: exec}
	err := client.PasteText(context.Background(), "maude-default:0.0", "hello\nworld")
	if err != nil {
		t.Fatalf("PasteText() error = %v", err)
	}
	if len(exec.calls) != 2 {
		t.Fatalf("calls = %#v", exec.calls)
	}
	if exec.calls[0].args[0] != "set-buffer" || exec.calls[0].args[3] != "hello\nworld" {
		t.Fatalf("set-buffer call = %#v", exec.calls[0])
	}
	wantPaste := []string{"paste-buffer", "-d", "-b", exec.calls[0].args[2], "-t", "maude-default:0.0"}
	if !reflect.DeepEqual(exec.calls[1].args, wantPaste) {
		t.Fatalf("paste args = %#v, want %#v", exec.calls[1].args, wantPaste)
	}
}

func TestHasSessionFalseOnTmuxExit(t *testing.T) {
	t.Parallel()

	exec := &fakeExec{errs: []error{ExitError{Err: errors.New("exit"), Stderr: "no session"}}}
	client := Client{Bin: "tmux", Exec: exec}
	got, err := client.HasSession(context.Background(), "missing")
	if err != nil {
		t.Fatalf("HasSession() error = %v", err)
	}
	if got {
		t.Fatal("HasSession() = true, want false")
	}
}
