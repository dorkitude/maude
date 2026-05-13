package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dorkitude/maude/internal/config"
	"github.com/dorkitude/maude/internal/maude"
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
	flags.BoolVarP(&opts.print, "print", "p", false, "Paste prompt into Claude TUI, capture pane output, and exit")
	flags.StringVar(&opts.session, "session", "", "Maude session name to route this prompt to")
	flags.StringVarP(&opts.resume, "resume", "r", "", "Claude conversation/session to resume inside the tmux pane")
	flags.BoolVar(&opts.noWait, "no-wait", false, "Paste prompt and return without waiting for pane capture")
	addCompatibilityFlags(flags)

	cmd.AddCommand(newStatusCommand(opts), newAttachCommand(opts), newResetCommand(opts))
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
	if err := config.Save(configPath(opts.configPath), cfg); err != nil {
		return err
	}
	store := state.New(cfg.StateDir)
	manager := maude.NewManager(cfg, store, tmux.New("tmux"))
	cwd, _ := os.Getwd()
	result, err := manager.RunPrint(context.Background(), maude.RunOptions{
		SessionName: opts.session,
		Resume:      opts.resume,
		Prompt:      prompt,
		ClaudeArgs:  forwardArgs(cmd.Flags()),
		NoWait:      opts.noWait,
		Cwd:         cwd,
	})
	if err != nil {
		return err
	}
	if result.Output != "" {
		fmt.Fprintln(cmd.OutOrStdout(), result.Output)
	}
	if result.Intervention != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "maude: %s\n", result.Intervention)
		fmt.Fprintf(cmd.ErrOrStderr(), "maude: run `maude attach --session %s` to intervene.\n", result.Session.Name)
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
