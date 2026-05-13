# maude

`maude` is a tiny `claude -p` compatibility shim for scripts, cronjobs, and shell pipelines.

Instead of starting Claude Code in deprecated print mode, `maude -p` queues the request for a small local daemon. The daemon keeps a normal Claude Code TUI alive in tmux, pastes an envelope into that pane, and tells the agent to complete the request with `maude agent print`. The goal is a one-letter migration path for common automation: change `claude -p` to `maude -p`.

![Maude architecture](docs/architecture.svg)

## Installation

### Homebrew (macOS/Linux)

```sh
brew tap dorkitude/maude
brew install maude
```

### From Source

```sh
git clone https://github.com/dorkitude/maude.git
cd maude
make build
make install
```

### For Development

```sh
git clone https://github.com/dorkitude/maude.git
cd maude
go run ./cmd/maude --help
make test
```

## Usage

Send a prompt to the default persistent Claude TUI:

```sh
maude -p "summarize this repository"
```

Pipe input the same way scripts commonly used `claude -p`:

```sh
git diff | maude -p "review this diff"
```

Choose a print output format:

```sh
maude -p --output-format text "summarize this repository"
maude -p --output-format json "summarize this repository"
maude -p --output-format stream-json "summarize this repository"
```

`text` is the default. `json` emits a Maude result object with the final answer in `result`. `stream-json` emits newline-delimited Maude events while `maude agent print` receives bytes from Claude's final response command.

## Supported Flags

Maude is intentionally not a complete `claude -p` reimplementation. It drives a persistent Claude Code TUI and receives the final answer through `maude agent print`, so only a small set of flags are supported and tested.

| Flag | Implementation |
| --- | --- |
| `-p`, `--print` | Maude-native. Enqueues the prompt, starts the daemon if needed, waits for `maude agent print`, then writes stdout. |
| prompt args | Maude-native. Positional args are joined with spaces. |
| stdin | Maude-native. If stdin is piped, it is appended after the prompt args with a newline. |
| `--session <name>` | Maude-native. Routes the request to a named tmux-backed Claude session. |
| `--resume`, `-r <id>` | Claude startup/restart flag. Starts or restarts the managed Claude TUI with `--resume <id>`. |
| `--no-wait` | Maude-native. Enqueues the request and prints the request ID without waiting for a response. |
| `--output-format text` | Maude-side formatting. The agent pipes only the final answer text to `maude agent print`; Maude prints that text. |
| `--output-format json` | Maude-side formatting. The agent pipes only the final answer text to `maude agent print`; Maude emits a JSON result object with the answer in `result`. |
| `--output-format stream-json` | Maude-side streaming. The agent streams only the final answer text to `maude agent print`; Maude emits newline-delimited JSON events as those bytes arrive, followed by a final result event. |
| `--model <model>` | Claude startup flag. Applied when the managed Claude TUI starts or restarts, not reliably per request on an already-running session. |
| `--permission-mode <mode>` | Claude startup flag. Applied when the managed Claude TUI starts or restarts. Maude also adds `--dangerously-skip-permissions` by default; see Permission Mode below. |
| `--tools <tools>` | Claude startup flag. Applied when the managed Claude TUI starts or restarts. |
| `--allowed-tools`, `--allowedTools <tools>` | Claude startup flag. Applied when the managed Claude TUI starts or restarts. |
| `--disallowed-tools`, `--disallowedTools <tools>` | Claude startup flag. Applied when the managed Claude TUI starts or restarts. |
| `--add-dir <dirs...>` | Claude startup flag. Applied when the managed Claude TUI starts or restarts. |
| `--mcp-config <configs...>` | Claude startup flag. Applied when the managed Claude TUI starts or restarts. |
| `--settings <file-or-json>` | Claude startup flag. Applied when the managed Claude TUI starts or restarts. |

Other Claude Code flags may be parsed for command-line compatibility, but they are not supported until they have explicit Maude behavior and repeatable tests. Native Claude usage metadata and schema validation are not automatically available through Maude's TUI/envelope response path.

Route work to a named Maude/tmux session:

```sh
maude -p --session nightly "run the nightly maintenance checklist"
```

Switch the underlying Claude conversation in that pane:

```sh
maude -p --session nightly --resume 018f... "continue from this Claude session"
```

Inspect, attach, or reset the tmux-backed session:

```sh
maude status
maude attach --session nightly
maude reset --session nightly
```

`maude` stores its JSON config in `state/config.json` by default and session metadata in `state/sessions/`. The `state/` directory is gitignored.

### Permission Mode

Maude automatically adds `--dangerously-skip-permissions` when it starts the managed Claude Code TUI:

```sh
claude --dangerously-skip-permissions
```

This is required for reliable unattended `maude -p` calls. Maude's response path asks Claude to run `maude agent print`; without skipped permissions, Claude can stop at a Bash approval prompt inside tmux and the waiting `maude -p` process will not receive a response.

Only run Maude in workspaces where you are comfortable giving the managed Claude session this level of access. To require manual approval instead, remove `--dangerously-skip-permissions` from `claude_args` in `state/config.json`; print-mode requests may then wait at Claude's permission prompt.

## Daemon Model

`maude -p` is a short-lived client. It writes a request into `state/maude.db`, starts the daemon if needed, waits for its request ID, and prints the response.

`maude daemon run` is the long-running worker. It is the only process that talks to tmux, so concurrent cronjobs and shell scripts do not race over the same Claude Code pane.

Claude receives an envelope that includes a command like:

```sh
maude agent print --request <id>
```

When Claude pipes its final answer to that command, the waiting `maude -p` process receives the matching output.

## Notes

Claude Code's TUI is not a machine-output protocol, so Maude avoids scrollback scraping for normal print responses. The TUI runtime sends the response back through `maude agent print`, which is much less fragile and lets multiple callers share the same long-running Claude Code session safely.

## Contributing

For pull requests that change CLI behavior, prompt envelopes, daemon processing, or output formatting, include the exact testing steps in the PR description.

List the commands you ran, the inputs you provided, and the outputs you observed. For `maude -p` compatibility work, include paired `claude -p` and `maude -p` examples whenever possible, including any stdin, flags, and stdout/stderr.
