package maude

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dorkitude/maude/internal/config"
	"github.com/dorkitude/maude/internal/state"
	"github.com/dorkitude/maude/internal/tmux"
)

type Tmux interface {
	HasSession(ctx context.Context, session string) (bool, error)
	NewSession(ctx context.Context, session string, cwd string, command []string) error
	SendKeys(ctx context.Context, target string, keys ...string) error
	PasteText(ctx context.Context, target string, text string) error
	CapturePane(ctx context.Context, target string, lines int) (string, error)
	KillSession(ctx context.Context, session string) error
}

type Manager struct {
	Config config.Config
	Store  state.Store
	Tmux   Tmux
	Now    func() time.Time
	Sleep  func(time.Duration)
}

type RunOptions struct {
	SessionName string
	Resume      string
	Prompt      string
	ClaudeArgs  []string
	NoWait      bool
	Cwd         string
}

type RunResult struct {
	Session      state.Session
	Output       string
	Intervention string
}

func NewManager(cfg config.Config, store state.Store, tc Tmux) Manager {
	if tc == nil {
		tc = tmux.New("tmux")
	}
	return Manager{
		Config: cfg,
		Store:  store,
		Tmux:   tc,
		Now:    func() time.Time { return time.Now().UTC() },
		Sleep:  time.Sleep,
	}
}

func (m Manager) RunPrint(ctx context.Context, opts RunOptions) (RunResult, error) {
	if strings.TrimSpace(opts.Prompt) == "" {
		return RunResult{}, errors.New("prompt is empty")
	}
	sess := m.sessionFor(opts.SessionName, opts.Resume)
	if existing, err := m.Store.LoadSession(sess.Name); err == nil {
		sess = existing
	} else if !os.IsNotExist(err) {
		return RunResult{}, fmt.Errorf("load session state: %w", err)
	}

	has, err := m.Tmux.HasSession(ctx, sess.TmuxSession)
	if err != nil {
		return RunResult{}, fmt.Errorf("check tmux session: %w", err)
	}
	if !has {
		if err := m.Tmux.NewSession(ctx, sess.TmuxSession, opts.Cwd, m.claudeCommand(opts.Resume, opts.ClaudeArgs)); err != nil {
			return RunResult{}, fmt.Errorf("start tmux session: %w", err)
		}
		sess.LastStatus = "created"
		sess.ClaudeResume = opts.Resume
		m.sleepConfig(m.Config.StartupWaitDuration)
	} else {
		sess.LastStatus = "reused"
	}

	if opts.Resume != "" && opts.Resume != sess.ClaudeResume {
		if err := m.switchResume(ctx, sess.PaneTarget, opts.Resume, opts.ClaudeArgs); err != nil {
			return RunResult{}, err
		}
		sess.ClaudeResume = opts.Resume
		sess.LastStatus = "resumed"
	}

	if err := m.Tmux.PasteText(ctx, sess.PaneTarget, opts.Prompt); err != nil {
		return RunResult{}, fmt.Errorf("paste prompt: %w", err)
	}
	if err := m.Tmux.SendKeys(ctx, sess.PaneTarget, "Enter"); err != nil {
		return RunResult{}, fmt.Errorf("submit prompt: %w", err)
	}
	sess.LastPromptAt = m.Now()

	result := RunResult{Session: sess}
	if !opts.NoWait {
		m.sleepConfig(m.Config.CaptureDelayDuration)
		output, err := m.captureStable(ctx, sess.PaneTarget)
		if err != nil {
			return RunResult{}, err
		}
		result.Output = strings.TrimRight(output, "\n")
		result.Intervention = detectIntervention(output)
		sess.LastCaptureAt = m.Now()
		sess.LastCaptureExcerpt = excerpt(output, 2000)
		if result.Intervention != "" {
			sess.LastStatus = "needs-intervention"
			sess.LastIntervention = result.Intervention
		}
	}

	result.Session = sess
	if err := m.Store.SaveSession(sess); err != nil {
		return RunResult{}, fmt.Errorf("save session state: %w", err)
	}
	return result, nil
}

func (m Manager) Reset(ctx context.Context, name string) error {
	sess := m.sessionFor(name, "")
	has, err := m.Tmux.HasSession(ctx, sess.TmuxSession)
	if err != nil {
		return err
	}
	if has {
		if err := m.Tmux.KillSession(ctx, sess.TmuxSession); err != nil {
			return err
		}
	}
	return m.Store.DeleteSession(sess.Name)
}

func (m Manager) sessionFor(name string, resume string) state.Session {
	if strings.TrimSpace(name) == "" {
		name = m.Config.DefaultSession
	}
	safe := state.SafeName(name)
	tmuxSession := m.Config.TmuxPrefix + "-" + safe
	now := m.Now()
	return state.Session{
		Name:         name,
		SafeName:     safe,
		TmuxSession:  tmuxSession,
		PaneTarget:   tmuxSession + ":0.0",
		ClaudeResume: resume,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func (m Manager) claudeCommand(resume string, extra []string) []string {
	bin := m.Config.ClaudeBinary
	if bin == "" {
		bin = "claude"
	}
	args := []string{bin}
	if resume != "" {
		args = append(args, "--resume", resume)
	}
	args = append(args, extra...)
	return args
}

func (m Manager) switchResume(ctx context.Context, target string, resume string, extra []string) error {
	if err := m.Tmux.SendKeys(ctx, target, "C-c"); err != nil {
		return fmt.Errorf("interrupt Claude for resume switch: %w", err)
	}
	m.sleepConfig(m.Config.CaptureDelayDuration)
	if err := m.Tmux.PasteText(ctx, target, tmux.ShellCommand(m.claudeCommand(resume, extra))); err != nil {
		return fmt.Errorf("paste resume command: %w", err)
	}
	if err := m.Tmux.SendKeys(ctx, target, "Enter"); err != nil {
		return fmt.Errorf("run resume command: %w", err)
	}
	m.sleepConfig(m.Config.StartupWaitDuration)
	return nil
}

func (m Manager) captureStable(ctx context.Context, target string) (string, error) {
	timeout, err := m.Config.WaitTimeoutDuration()
	if err != nil {
		return "", err
	}
	poll, err := m.Config.WaitPollIntervalDuration()
	if err != nil {
		return "", err
	}
	deadline := m.Now().Add(timeout)
	var previous string
	for {
		current, err := m.Tmux.CapturePane(ctx, target, m.Config.CaptureLines)
		if err != nil {
			return "", fmt.Errorf("capture pane: %w", err)
		}
		if current == previous && current != "" {
			return current, nil
		}
		if !m.Now().Before(deadline) {
			return current, nil
		}
		previous = current
		m.sleep(poll)
	}
}

func (m Manager) sleepConfig(fn func() (time.Duration, error)) {
	d, err := fn()
	if err == nil {
		m.sleep(d)
	}
}

func (m Manager) sleep(d time.Duration) {
	if d <= 0 {
		return
	}
	if m.Sleep == nil {
		time.Sleep(d)
		return
	}
	m.Sleep(d)
}

func detectIntervention(output string) string {
	lower := strings.ToLower(output)
	patterns := []struct {
		needle string
		msg    string
	}{
		{"session expired", "Claude session appears to be expired; attach to the tmux session and sign in again."},
		{"login required", "Claude appears to require login; attach to the tmux session and complete authentication."},
		{"please log in", "Claude appears to require login; attach to the tmux session and complete authentication."},
		{"authentication", "Claude appears to require authentication; attach to the tmux session and complete it."},
		{"trust this directory", "Claude is waiting for workspace trust confirmation in the tmux session."},
	}
	for _, pattern := range patterns {
		if strings.Contains(lower, pattern.needle) {
			return pattern.msg
		}
	}
	return ""
}

func excerpt(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[len(s)-max:]
}
