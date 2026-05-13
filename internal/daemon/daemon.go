package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/dorkitude/maude/internal/config"
	"github.com/dorkitude/maude/internal/envelope"
	"github.com/dorkitude/maude/internal/maude"
	"github.com/dorkitude/maude/internal/queue"
	"github.com/dorkitude/maude/internal/state"
	"github.com/dorkitude/maude/internal/tmux"
)

type Service struct {
	ConfigPath string
	Config     config.Config
	Store      state.Store
	Executable string
}

func New(configPath string, cfg config.Config) Service {
	exe, _ := os.Executable()
	return Service{
		ConfigPath: canonicalConfigPath(configPath),
		Config:     cfg,
		Store:      state.New(cfg.StateDir),
		Executable: exe,
	}
}

func (s Service) Start() error {
	if running, _ := s.Running(); running {
		return nil
	}
	if err := os.MkdirAll(s.Store.Root, 0o755); err != nil {
		return err
	}
	logPath := filepath.Join(s.Store.Root, "mauded.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()

	args := []string{"--config", s.ConfigPath, "daemon", "run"}
	cmd := exec.Command(s.Executable, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if running, _ := s.Running(); running {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("daemon did not start; see %s", logPath)
}

func (s Service) Stop() error {
	pid, err := readPID(s.Store.PIDPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	_ = os.Remove(s.Store.PIDPath())
	return nil
}

func (s Service) Running() (bool, int) {
	pid, err := readPID(s.Store.PIDPath())
	if err != nil || pid <= 0 {
		return false, 0
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false, 0
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return false, pid
	}
	return true, pid
}

func (s Service) Run(ctx context.Context) error {
	lock, err := acquireLock(s.Store.LockPath())
	if err != nil {
		return err
	}
	defer lock.Close()

	if err := os.WriteFile(s.Store.PIDPath(), []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o644); err != nil {
		return err
	}
	defer os.Remove(s.Store.PIDPath())

	q, err := queue.Open(s.Store.DBPath())
	if err != nil {
		return err
	}
	defer q.Close()

	manager := maude.NewManager(s.Config, s.Store, tmux.New("tmux"))
	poll, err := s.Config.DaemonPollDuration()
	if err != nil {
		return err
	}
	if poll <= 0 {
		poll = 500 * time.Millisecond
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req, ok, err := q.NextQueued()
		if err != nil {
			return err
		}
		if !ok {
			time.Sleep(poll)
			continue
		}
		if _, err := q.MarkRunning(req.ID); err != nil {
			continue
		}
		if err := s.process(ctx, q, manager, req); err != nil {
			_, _ = q.Fail(req.ID, err.Error(), false)
		}
	}
}

func (s Service) process(ctx context.Context, q *queue.Queue, manager maude.Manager, req queue.Request) error {
	env, err := envelope.BuildPrintRequest(envelope.PrintRequest{
		RequestID:    req.ID,
		Message:      req.Prompt,
		RespondWith:  fmt.Sprintf("%s --config %s agent print --request %s", shellQuote(s.Executable), shellQuote(s.ConfigPath), shellQuote(req.ID)),
		OutputFormat: req.OutputFormat,
	})
	if err != nil {
		return err
	}
	_, err = manager.Submit(ctx, maude.RunOptions{
		SessionName: req.SessionName,
		Resume:      req.Resume,
		Prompt:      env,
		ClaudeArgs:  req.ClaudeArgs,
		NoWait:      true,
		Cwd:         req.Cwd,
	})
	if err != nil {
		return err
	}

	timeout, err := s.Config.RequestTimeoutDuration()
	if err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	for {
		current, err := q.Get(req.ID)
		if err != nil {
			return err
		}
		switch current.Status {
		case queue.StatusDone, queue.StatusFailed, queue.StatusNeedsIntervention:
			return nil
		}
		if !deadline.IsZero() && time.Now().After(deadline) {
			_, _ = q.Fail(req.ID, "timed out waiting for maude agent print response", true)
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func canonicalConfigPath(path string) string {
	if path == "" {
		return config.DefaultPath()
	}
	return path
}

func readPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var pid int
	if _, err := fmt.Sscanf(string(data), "%d", &pid); err != nil {
		return 0, err
	}
	return pid, nil
}

type lockFile struct {
	f *os.File
}

func acquireLock(path string) (*lockFile, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("maude daemon is already running")
	}
	return &lockFile{f: f}, nil
}

func (l *lockFile) Close() error {
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	return l.f.Close()
}

func shellQuote(s string) string {
	return tmux.ShellCommand([]string{s})
}
