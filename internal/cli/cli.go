package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/dorkitude/maude/internal/config"
	"github.com/dorkitude/maude/internal/daemon"
	"github.com/dorkitude/maude/internal/maude"
	"github.com/dorkitude/maude/internal/queue"
	"github.com/dorkitude/maude/internal/state"
	"github.com/dorkitude/maude/internal/tmux"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type rootOptions struct {
	configPath string
	print      bool
	session    string
	resume     string
	noWait     bool
}

type flagSpec struct {
	name      string
	shorthand string
	kind      string
	forward   bool
}

var compatibilityFlags = []flagSpec{
	{name: "add-dir", kind: "stringSlice", forward: true},
	{name: "agent", kind: "string", forward: true},
	{name: "agents", kind: "string", forward: true},
	{name: "allow-dangerously-skip-permissions", kind: "bool", forward: true},
	{name: "allowedTools", kind: "stringSlice", forward: true},
	{name: "allowed-tools", kind: "stringSlice", forward: true},
	{name: "append-system-prompt", kind: "string", forward: true},
	{name: "bare", kind: "bool", forward: true},
	{name: "betas", kind: "stringSlice", forward: true},
	{name: "brief", kind: "bool", forward: true},
	{name: "chrome", kind: "bool", forward: true},
	{name: "continue", shorthand: "c", kind: "bool", forward: true},
	{name: "dangerously-skip-permissions", kind: "bool", forward: true},
	{name: "debug", shorthand: "d", kind: "optionalString", forward: true},
	{name: "debug-file", kind: "string", forward: true},
	{name: "disable-slash-commands", kind: "bool", forward: true},
	{name: "disallowedTools", kind: "stringSlice", forward: true},
	{name: "disallowed-tools", kind: "stringSlice", forward: true},
	{name: "effort", kind: "string", forward: true},
	{name: "exclude-dynamic-system-prompt-sections", kind: "bool", forward: true},
	{name: "fallback-model", kind: "string", forward: false},
	{name: "file", kind: "stringSlice", forward: true},
	{name: "fork-session", kind: "bool", forward: true},
	{name: "from-pr", kind: "optionalString", forward: true},
	{name: "ide", kind: "bool", forward: true},
	{name: "include-hook-events", kind: "bool", forward: false},
	{name: "include-partial-messages", kind: "bool", forward: false},
	{name: "input-format", kind: "string", forward: false},
	{name: "json-schema", kind: "string", forward: false},
	{name: "max-budget-usd", kind: "string", forward: false},
	{name: "mcp-config", kind: "stringSlice", forward: true},
	{name: "mcp-debug", kind: "bool", forward: true},
	{name: "model", kind: "string", forward: true},
	{name: "name", shorthand: "n", kind: "string", forward: true},
	{name: "no-chrome", kind: "bool", forward: true},
	{name: "no-session-persistence", kind: "bool", forward: false},
	{name: "output-format", kind: "string", forward: false},
	{name: "permission-mode", kind: "string", forward: true},
	{name: "plugin-dir", kind: "stringArray", forward: true},
	{name: "plugin-url", kind: "stringArray", forward: true},
	{name: "remote-control", kind: "optionalString", forward: true},
	{name: "remote-control-session-name-prefix", kind: "string", forward: true},
	{name: "replay-user-messages", kind: "bool", forward: false},
	{name: "session-id", kind: "string", forward: true},
	{name: "setting-sources", kind: "string", forward: true},
	{name: "settings", kind: "string", forward: true},
	{name: "strict-mcp-config", kind: "bool", forward: true},
	{name: "system-prompt", kind: "string", forward: true},
	{name: "tools", kind: "stringSlice", forward: true},
	{name: "verbose", kind: "bool", forward: true},
	{name: "worktree", shorthand: "w", kind: "optionalString", forward: true},
}

func NewRootCommand() *cobra.Command {
	opts := &rootOptions{}
	cmd := &cobra.Command{
		Use:           "maude [flags] [prompt]",
		Short:         "Route claude -p style prompts into a persistent Claude Code tmux TUI",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !opts.print {
				return cmd.Help()
			}
			return runPrint(cmd, opts, args)
		},
	}
	cmd.FParseErrWhitelist.UnknownFlags = true

	cmd.PersistentFlags().StringVar(&opts.configPath, "config", "", "Path to Maude JSON config file")

	flags := cmd.Flags()
	flags.BoolVarP(&opts.print, "print", "p", false, "Queue prompt for Claude TUI, wait for agent print response, and exit")
	flags.StringVar(&opts.session, "session", "", "Maude session name to route this prompt to")
	flags.StringVarP(&opts.resume, "resume", "r", "", "Claude conversation/session to resume inside the tmux pane")
	flags.BoolVar(&opts.noWait, "no-wait", false, "Queue prompt and return the request ID without waiting for a response")
	addCompatibilityFlags(flags)

	cmd.AddCommand(newStatusCommand(opts), newAttachCommand(opts), newResetCommand(opts), newAgentCommand(opts), newDaemonCommand(opts))
	return cmd
}

func runPrint(cmd *cobra.Command, opts *rootOptions, args []string) error {
	prompt, err := CollectPrompt(args, cmd.InOrStdin())
	if err != nil {
		return err
	}
	cfg, err := config.Load(opts.configPath)
	if err != nil {
		return err
	}
	outputFormat, err := outputFormat(cmd.Flags())
	if err != nil {
		return err
	}
	if err := config.Save(configPath(opts.configPath), cfg); err != nil {
		return err
	}
	store := state.New(cfg.StateDir)
	q, err := queue.Open(store.DBPath())
	if err != nil {
		return err
	}
	defer q.Close()

	cwd, _ := os.Getwd()
	req, err := q.Enqueue(queue.Request{
		SessionName:  opts.session,
		Resume:       opts.resume,
		Prompt:       prompt,
		ClaudeArgs:   forwardArgs(cmd.Flags()),
		OutputFormat: outputFormat,
		Cwd:          cwd,
	})
	if err != nil {
		return err
	}
	if err := daemon.New(opts.configPath, cfg).Start(); err != nil {
		return err
	}
	if opts.noWait {
		fmt.Fprintln(cmd.OutOrStdout(), req.ID)
		return nil
	}
	if outputFormat == "stream-json" {
		return streamRequestOutput(cmd.OutOrStdout(), q, req.ID, cfg)
	}
	result, err := waitForRequest(q, req.ID, cfg)
	if err != nil {
		return err
	}
	output, err := formatRequestOutput(result)
	if err != nil {
		return err
	}
	if output != "" {
		fmt.Fprintln(cmd.OutOrStdout(), output)
	}
	return nil
}

func newStatusCommand(parent *rootOptions) *cobra.Command {
	var session string
	cmd := &cobra.Command{
		Use:          "status",
		Short:        "Show Maude session state",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(parent.configPath)
			if err != nil {
				return err
			}
			store := state.New(cfg.StateDir)
			if session != "" {
				sess, err := store.LoadSession(session)
				if err != nil {
					return err
				}
				printSession(cmd.OutOrStdout(), sess)
				return nil
			}
			sessions, err := store.ListSessions()
			if err != nil {
				return err
			}
			if len(sessions) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no sessions")
				return nil
			}
			for _, sess := range sessions {
				printSession(cmd.OutOrStdout(), sess)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "Maude session name")
	return cmd
}

func newAttachCommand(parent *rootOptions) *cobra.Command {
	var session string
	cmd := &cobra.Command{
		Use:          "attach",
		Short:        "Attach to a Maude tmux session",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(parent.configPath)
			if err != nil {
				return err
			}
			if session == "" {
				session = cfg.DefaultSession
			}
			tmuxSession := cfg.TmuxPrefix + "-" + state.SafeName(session)
			args = []string{"attach-session", "-t", tmuxSession}
			c := exec.Command("tmux", args...)
			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			return c.Run()
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "Maude session name")
	return cmd
}

func newResetCommand(parent *rootOptions) *cobra.Command {
	var session string
	cmd := &cobra.Command{
		Use:          "reset",
		Short:        "Kill a Maude tmux session and delete its state",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(parent.configPath)
			if err != nil {
				return err
			}
			if session == "" {
				session = cfg.DefaultSession
			}
			manager := maude.NewManager(cfg, state.New(cfg.StateDir), tmux.New("tmux"))
			if err := manager.Reset(context.Background(), session); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "reset %s\n", session)
			return nil
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "Maude session name")
	return cmd
}

func newAgentCommand(parent *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "agent",
		Short:        "Agent-facing commands used by pasted Maude envelopes",
		SilenceUsage: true,
	}
	cmd.AddCommand(newAgentPrintCommand(parent))
	return cmd
}

func newAgentPrintCommand(parent *rootOptions) *cobra.Command {
	var requestID string
	cmd := &cobra.Command{
		Use:          "print",
		Short:        "Complete a queued print request from stdin",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if requestID == "" {
				return fmt.Errorf("--request is required")
			}
			cfg, err := config.Load(parent.configPath)
			if err != nil {
				return err
			}
			q, err := queue.Open(state.New(cfg.StateDir).DBPath())
			if err != nil {
				return err
			}
			defer q.Close()
			req, err := q.Get(requestID)
			if err != nil {
				return err
			}
			if req.OutputFormat == "stream-json" {
				return streamAgentPrint(cmd.InOrStdin(), q, requestID)
			}
			data, err := io.ReadAll(cmd.InOrStdin())
			if err != nil {
				return err
			}
			_, err = q.Complete(requestID, strings.TrimRight(string(data), "\n"))
			return err
		},
	}
	cmd.Flags().StringVar(&requestID, "request", "", "Queued Maude request ID")
	return cmd
}

func newDaemonCommand(parent *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "daemon",
		Short:        "Manage the Maude background worker",
		SilenceUsage: true,
	}
	cmd.AddCommand(newDaemonRunCommand(parent), newDaemonStartCommand(parent), newDaemonStatusCommand(parent), newDaemonStopCommand(parent))
	return cmd
}

func newDaemonRunCommand(parent *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:          "run",
		Short:        "Run the Maude daemon in the foreground",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(parent.configPath)
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			err = daemon.New(parent.configPath, cfg).Run(ctx)
			if errorsIsContextDone(err) {
				return nil
			}
			return err
		},
	}
}

func newDaemonStartCommand(parent *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:          "start",
		Short:        "Start the Maude daemon in the background",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(parent.configPath)
			if err != nil {
				return err
			}
			if err := config.Save(configPath(parent.configPath), cfg); err != nil {
				return err
			}
			if err := daemon.New(parent.configPath, cfg).Start(); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "maude daemon started")
			return nil
		},
	}
}

func newDaemonStatusCommand(parent *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:          "status",
		Short:        "Show Maude daemon status",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(parent.configPath)
			if err != nil {
				return err
			}
			running, pid := daemon.New(parent.configPath, cfg).Running()
			if running {
				fmt.Fprintf(cmd.OutOrStdout(), "running pid=%d\n", pid)
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "stopped")
			}
			return nil
		},
	}
}

func newDaemonStopCommand(parent *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:          "stop",
		Short:        "Stop the Maude daemon",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(parent.configPath)
			if err != nil {
				return err
			}
			if err := daemon.New(parent.configPath, cfg).Stop(); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "maude daemon stopped")
			return nil
		},
	}
}

func CollectPrompt(args []string, in io.Reader) (string, error) {
	parts := make([]string, 0, 2)
	if len(args) > 0 {
		parts = append(parts, strings.Join(args, " "))
	}
	if stdinHasData(in) {
		data, err := io.ReadAll(in)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		if text := strings.TrimRight(string(data), "\n"); text != "" {
			parts = append(parts, text)
		}
	}
	prompt := strings.TrimSpace(strings.Join(parts, "\n"))
	if prompt == "" {
		return "", fmt.Errorf("no prompt provided")
	}
	return prompt, nil
}

func streamAgentPrint(in io.Reader, q *queue.Queue, requestID string) error {
	reader := bufio.NewReader(in)
	var output strings.Builder
	buf := make([]byte, 1024)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			chunk := string(buf[:n])
			output.WriteString(chunk)
			if _, appendErr := q.AppendOutput(requestID, chunk); appendErr != nil {
				return appendErr
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	_, err := q.Complete(requestID, strings.TrimRight(output.String(), "\n"))
	return err
}

func outputFormat(flags *pflag.FlagSet) (string, error) {
	value, err := flags.GetString("output-format")
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "text", nil
	}
	switch value {
	case "text", "json", "stream-json":
		return value, nil
	default:
		return "", fmt.Errorf("--output-format must be one of: text, json, stream-json")
	}
}

func formatRequestOutput(req queue.Request) (string, error) {
	format := strings.TrimSpace(req.OutputFormat)
	if format == "" {
		format = "text"
	}
	switch format {
	case "text":
		return req.Output, nil
	case "json":
		data, err := json.Marshal(map[string]any{
			"type":       "result",
			"subtype":    "success",
			"is_error":   false,
			"request_id": req.ID,
			"result":     req.Output,
		})
		if err != nil {
			return "", err
		}
		return string(data), nil
	case "stream-json":
		assistant, err := streamAssistantEvent(req.ID, req.Output)
		if err != nil {
			return "", err
		}
		result, err := streamResultEvent(req)
		if err != nil {
			return "", err
		}
		return assistant + "\n" + result, nil
	default:
		return "", fmt.Errorf("unsupported output format %q", format)
	}
}

func streamRequestOutput(w io.Writer, q *queue.Queue, id string, cfg config.Config) error {
	timeout, err := cfg.RequestTimeoutDuration()
	if err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	printedInit := false
	printedBytes := 0
	for {
		req, err := q.Get(id)
		if err != nil {
			return err
		}
		if !printedInit {
			if err := writeStreamLine(w, map[string]any{
				"type":       "system",
				"subtype":    "init",
				"request_id": req.ID,
				"cwd":        req.Cwd,
			}); err != nil {
				return err
			}
			printedInit = true
		}
		if len(req.Output) > printedBytes {
			chunk := req.Output[printedBytes:]
			line, err := streamAssistantEvent(req.ID, chunk)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintln(w, line); err != nil {
				return err
			}
			printedBytes = len(req.Output)
		}
		switch req.Status {
		case queue.StatusDone:
			line, err := streamResultEvent(req)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(w, line)
			return err
		case queue.StatusFailed:
			return fmt.Errorf("%s", req.Error)
		case queue.StatusNeedsIntervention:
			return fmt.Errorf("%s; run `maude attach --session %s` to intervene", req.Intervention, req.SessionName)
		}
		if timeout > 0 && time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for request %s", id)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func streamAssistantEvent(requestID string, text string) (string, error) {
	data, err := json.Marshal(map[string]any{
		"type":       "assistant",
		"request_id": requestID,
		"message": map[string]any{
			"role": "assistant",
			"content": []map[string]string{
				{"type": "text", "text": text},
			},
		},
	})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func streamResultEvent(req queue.Request) (string, error) {
	data, err := json.Marshal(map[string]any{
		"type":       "result",
		"subtype":    "success",
		"is_error":   false,
		"request_id": req.ID,
		"result":     req.Output,
	})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func writeStreamLine(w io.Writer, value map[string]any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}

func waitForRequest(q *queue.Queue, id string, cfg config.Config) (queue.Request, error) {
	timeout, err := cfg.RequestTimeoutDuration()
	if err != nil {
		return queue.Request{}, err
	}
	deadline := time.Now().Add(timeout)
	for {
		req, err := q.Get(id)
		if err != nil {
			return queue.Request{}, err
		}
		switch req.Status {
		case queue.StatusDone:
			return req, nil
		case queue.StatusFailed:
			return queue.Request{}, fmt.Errorf("%s", req.Error)
		case queue.StatusNeedsIntervention:
			return queue.Request{}, fmt.Errorf("%s; run `maude attach --session %s` to intervene", req.Intervention, req.SessionName)
		}
		if timeout > 0 && time.Now().After(deadline) {
			return queue.Request{}, fmt.Errorf("timed out waiting for request %s", id)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func errorsIsContextDone(err error) bool {
	return err == nil || err == context.Canceled || err == context.DeadlineExceeded
}

func stdinHasData(in io.Reader) bool {
	file, ok := in.(*os.File)
	if !ok {
		return true
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice == 0
}

func addCompatibilityFlags(flags *pflag.FlagSet) {
	for _, spec := range compatibilityFlags {
		name := spec.name
		switch spec.kind {
		case "bool":
			flags.BoolP(name, spec.shorthand, false, "Claude compatibility flag")
		case "string":
			flags.StringP(name, spec.shorthand, "", "Claude compatibility flag")
		case "optionalString":
			flags.StringP(name, spec.shorthand, "", "Claude compatibility flag")
			flags.Lookup(name).NoOptDefVal = "true"
		case "stringSlice":
			flags.StringSliceP(name, spec.shorthand, nil, "Claude compatibility flag")
		case "stringArray":
			flags.StringArrayP(name, spec.shorthand, nil, "Claude compatibility flag")
		}
	}
}

func forwardArgs(flags *pflag.FlagSet) []string {
	var out []string
	for _, spec := range compatibilityFlags {
		if !spec.forward {
			continue
		}
		flag := flags.Lookup(spec.name)
		if flag == nil || !flag.Changed {
			continue
		}
		name := "--" + spec.name
		switch spec.kind {
		case "bool":
			out = append(out, name)
		case "string", "optionalString":
			value := flag.Value.String()
			if spec.kind == "optionalString" && value == "true" {
				out = append(out, name)
			} else {
				out = append(out, name, value)
			}
		case "stringSlice", "stringArray":
			values, err := flags.GetStringSlice(spec.name)
			if spec.kind == "stringArray" {
				values, err = flags.GetStringArray(spec.name)
			}
			if err != nil {
				continue
			}
			for _, value := range values {
				out = append(out, name, value)
			}
		}
	}
	return out
}

func printSession(w io.Writer, sess state.Session) {
	fmt.Fprintf(w, "%s\t%s\t%s", sess.Name, sess.TmuxSession, sess.LastStatus)
	if sess.ClaudeResume != "" {
		fmt.Fprintf(w, "\tresume=%s", sess.ClaudeResume)
	}
	if sess.LastIntervention != "" {
		fmt.Fprintf(w, "\t%s", sess.LastIntervention)
	}
	fmt.Fprintln(w)
}

func configPath(path string) string {
	if path != "" {
		return path
	}
	return filepath.Join(config.DefaultStateDir, config.DefaultConfigFileName)
}
