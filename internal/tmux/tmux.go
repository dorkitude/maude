package tmux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Executor runs tmux commands. It is injectable so tests do not need tmux.
type Executor interface {
	Run(ctx context.Context, name string, args ...string) (string, error)
}

type ExitError struct {
	Err    error
	Stderr string
}

func (e ExitError) Error() string {
	if e.Stderr == "" {
		return e.Err.Error()
	}
	return strings.TrimSpace(e.Stderr)
}

func (e ExitError) Unwrap() error { return e.Err }

type OSExecutor struct{}

func (OSExecutor) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), ExitError{Err: err, Stderr: stderr.String()}
	}
	return stdout.String(), nil
}

type Client struct {
	Bin     string
	Exec    Executor
	Timeout time.Duration
}

func New(bin string) Client {
	if bin == "" {
		bin = "tmux"
	}
	return Client{Bin: bin, Exec: OSExecutor{}, Timeout: 10 * time.Second}
}

func (c Client) HasSession(ctx context.Context, session string) (bool, error) {
	_, err := c.run(ctx, "has-session", "-t", session)
	if err == nil {
		return true, nil
	}
	var exit ExitError
	if errors.As(err, &exit) {
		return false, nil
	}
	return false, err
}

func (c Client) NewSession(ctx context.Context, session string, cwd string, command []string) error {
	args := []string{"new-session", "-d", "-s", session}
	if cwd != "" {
		args = append(args, "-c", cwd)
	}
	if len(command) > 0 {
		args = append(args, ShellCommand(command))
	}
	_, err := c.run(ctx, args...)
	return err
}

func (c Client) SendKeys(ctx context.Context, target string, keys ...string) error {
	args := append([]string{"send-keys", "-t", target}, keys...)
	_, err := c.run(ctx, args...)
	return err
}

func (c Client) PasteText(ctx context.Context, target string, text string) error {
	buffer := fmt.Sprintf("maude-%d", time.Now().UnixNano())
	if _, err := c.run(ctx, "set-buffer", "-b", buffer, text); err != nil {
		return err
	}
	_, err := c.run(ctx, "paste-buffer", "-d", "-b", buffer, "-t", target)
	return err
}

func (c Client) CapturePane(ctx context.Context, target string, lines int) (string, error) {
	if lines <= 0 {
		lines = 200
	}
	return c.run(ctx, "capture-pane", "-p", "-t", target, "-S", "-"+strconv.Itoa(lines))
}

func (c Client) DisplayMessage(ctx context.Context, target string, format string) (string, error) {
	return c.run(ctx, "display-message", "-p", "-t", target, format)
}

func (c Client) KillSession(ctx context.Context, session string) error {
	_, err := c.run(ctx, "kill-session", "-t", session)
	return err
}

func (c Client) AttachArgs(session string) []string {
	return []string{"attach-session", "-t", session}
}

func (c Client) run(ctx context.Context, args ...string) (string, error) {
	if c.Exec == nil {
		c.Exec = OSExecutor{}
	}
	if c.Bin == "" {
		c.Bin = "tmux"
	}
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	return c.Exec.Run(ctx, c.Bin, args...)
}

func ShellCommand(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./:=,+@", r))
	}) == -1 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
