package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dorkitude/maude/internal/config"
	"github.com/dorkitude/maude/internal/queue"
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

func TestOutputFormatValidation(t *testing.T) {
	t.Parallel()

	cmd := NewRootCommand()
	cmd.SetArgs([]string{"-p", "--output-format", "json", "hello"})
	if err := cmd.Flags().Parse([]string{"-p", "--output-format", "json", "hello"}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	got, err := outputFormat(cmd.Flags())
	if err != nil {
		t.Fatalf("outputFormat() error = %v", err)
	}
	if got != "json" {
		t.Fatalf("outputFormat() = %q, want json", got)
	}

	if err := cmd.Flags().Set("output-format", "xml"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if _, err := outputFormat(cmd.Flags()); err == nil {
		t.Fatal("outputFormat() error = nil, want invalid format error")
	}
}

func TestFormatRequestOutputJSON(t *testing.T) {
	t.Parallel()

	got, err := formatRequestOutput(queue.Request{ID: "abc", Output: "answer", OutputFormat: "json"})
	if err != nil {
		t.Fatalf("formatRequestOutput() error = %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(got), &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v; output=%s", err, got)
	}
	if payload["type"] != "result" || payload["result"] != "answer" || payload["request_id"] != "abc" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestFormatRequestOutputStreamJSON(t *testing.T) {
	t.Parallel()

	got, err := formatRequestOutput(queue.Request{ID: "abc", Output: "answer", OutputFormat: "stream-json"})
	if err != nil {
		t.Fatalf("formatRequestOutput() error = %v", err)
	}
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("stream-json lines = %d, output=%s", len(lines), got)
	}
	for _, line := range lines {
		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			t.Fatalf("Unmarshal(%q) error = %v", line, err)
		}
	}
}

func TestStreamRequestOutputEmitsChunksAsTheyArrive(t *testing.T) {
	t.Parallel()

	q, err := queue.Open(t.TempDir() + "/maude.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer q.Close()
	req, err := q.Enqueue(queue.Request{Prompt: "hello", OutputFormat: "stream-json", Cwd: "/tmp/project"})
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if _, err := q.MarkRunning(req.ID); err != nil {
		t.Fatalf("MarkRunning() error = %v", err)
	}

	go func() {
		time.Sleep(150 * time.Millisecond)
		_, _ = q.AppendOutput(req.ID, "alpha")
		time.Sleep(150 * time.Millisecond)
		_, _ = q.AppendOutput(req.ID, " beta")
		time.Sleep(150 * time.Millisecond)
		_, _ = q.Complete(req.ID, "alpha beta")
	}()

	cfg := config.Defaults()
	cfg.RequestTimeout = "2s"
	var out bytes.Buffer
	if err := streamRequestOutput(&out, q, req.ID, cfg); err != nil {
		t.Fatalf("streamRequestOutput() error = %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("got %d stream lines, want 4:\n%s", len(lines), out.String())
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("Unmarshal(init) error = %v", err)
	}
	if first["type"] != "system" || first["subtype"] != "init" {
		t.Fatalf("init event = %#v", first)
	}
	if !strings.Contains(out.String(), "alpha") || !strings.Contains(out.String(), " beta") {
		t.Fatalf("stream output missing chunks:\n%s", out.String())
	}
}

func TestStreamAgentPrintAppendsOutputBeforeComplete(t *testing.T) {
	t.Parallel()

	q, err := queue.Open(t.TempDir() + "/maude.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer q.Close()
	req, err := q.Enqueue(queue.Request{Prompt: "hello", OutputFormat: "stream-json"})
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if _, err := q.MarkRunning(req.ID); err != nil {
		t.Fatalf("MarkRunning() error = %v", err)
	}
	if err := streamAgentPrint(strings.NewReader("hello\n"), q, req.ID); err != nil {
		t.Fatalf("streamAgentPrint() error = %v", err)
	}
	got, err := q.Get(req.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status != queue.StatusDone || got.Output != "hello" {
		t.Fatalf("request = %#v", got)
	}
}
