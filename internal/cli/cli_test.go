package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestCollectPromptFromArgs(t *testing.T) {
	t.Parallel()

	got, err := CollectPrompt([]string{"hello", "world"}, strings.NewReader(""))
	if err != nil {
		t.Fatalf("CollectPrompt() error = %v", err)
	}
	if got != "hello world" {
		t.Fatalf("prompt = %q", got)
	}
}

func TestCollectPromptCombinesArgsAndStdin(t *testing.T) {
	t.Parallel()

	got, err := CollectPrompt([]string{"review"}, strings.NewReader("diff\n"))
	if err != nil {
		t.Fatalf("CollectPrompt() error = %v", err)
	}
	if got != "review\ndiff" {
		t.Fatalf("prompt = %q", got)
	}
}

func TestRootParsesCompatibilityFlags(t *testing.T) {
	t.Parallel()

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"-p", "--model", "sonnet", "--permission-mode", "plan", "--no-wait", "hello"})
	if err := cmd.Flags().Parse([]string{"-p", "--model", "sonnet", "--permission-mode", "plan", "--no-wait", "hello"}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	args := forwardArgs(cmd.Flags())
	want := []string{"--model", "sonnet", "--permission-mode", "plan"}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("forwardArgs() = %#v, want %#v", args, want)
	}
}
