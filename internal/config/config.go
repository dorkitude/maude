package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"
)

const (
	DefaultStateDir       = "state"
	DefaultConfigFileName = "config.json"
)

// Config is the user-editable Maude configuration persisted as JSON.
type Config struct {
	StateDir       string   `json:"state_dir" mapstructure:"state_dir"`
	DefaultSession string   `json:"default_session" mapstructure:"default_session"`
	TmuxPrefix     string   `json:"tmux_prefix" mapstructure:"tmux_prefix"`
	ClaudeBinary   string   `json:"claude_binary" mapstructure:"claude_binary"`
	ClaudeArgs     []string `json:"claude_args" mapstructure:"claude_args"`

	StartupWait      string `json:"startup_wait" mapstructure:"startup_wait"`
	WaitTimeout      string `json:"wait_timeout" mapstructure:"wait_timeout"`
	WaitPollInterval string `json:"wait_poll_interval" mapstructure:"wait_poll_interval"`
	CaptureDelay     string `json:"capture_delay" mapstructure:"capture_delay"`
	CaptureLines     int    `json:"capture_lines" mapstructure:"capture_lines"`
	DaemonPoll       string `json:"daemon_poll" mapstructure:"daemon_poll"`
	RequestTimeout   string `json:"request_timeout" mapstructure:"request_timeout"`
	ReadyMarker      string `json:"ready_marker" mapstructure:"ready_marker"`
	ReadyTimeout     string `json:"ready_timeout" mapstructure:"ready_timeout"`
}

// Defaults returns Maude's built-in configuration defaults.
func Defaults() Config {
	return Config{
		StateDir:         DefaultStateDir,
		DefaultSession:   "default",
		TmuxPrefix:       "maude",
		ClaudeBinary:     "claude",
		ClaudeArgs:       []string{"--dangerously-skip-permissions"},
		StartupWait:      "2s",
		WaitTimeout:      "30s",
		WaitPollInterval: "500ms",
		CaptureDelay:     "250ms",
		CaptureLines:     200,
		DaemonPoll:       "500ms",
		RequestTimeout:   "10m",
		ReadyMarker:      "Try \"",
		ReadyTimeout:     "15s",
	}
}

// DefaultPath is the config file path used when no override is supplied.
func DefaultPath() string {
	return filepath.Join(DefaultStateDir, DefaultConfigFileName)
}

// Load reads config from path, applying defaults for missing values. If path is
// empty, state/config.json is used. A missing config file is not an error.
func Load(path string) (Config, error) {
	v := newViper(path)
	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			if _, statErr := os.Stat(configPath(path)); !errors.Is(statErr, os.ErrNotExist) {
				return Config{}, fmt.Errorf("read config: %w", err)
			}
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	return cfg, nil
}

// Save writes cfg as pretty JSON. If path is empty, state/config.json is used.
func Save(path string, cfg Config) error {
	path = configPath(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func (c Config) StartupWaitDuration() (time.Duration, error) {
	return parseDuration("startup_wait", c.StartupWait)
}

func (c Config) WaitTimeoutDuration() (time.Duration, error) {
	return parseDuration("wait_timeout", c.WaitTimeout)
}

func (c Config) WaitPollIntervalDuration() (time.Duration, error) {
	return parseDuration("wait_poll_interval", c.WaitPollInterval)
}

func (c Config) CaptureDelayDuration() (time.Duration, error) {
	return parseDuration("capture_delay", c.CaptureDelay)
}

func (c Config) DaemonPollDuration() (time.Duration, error) {
	return parseDuration("daemon_poll", c.DaemonPoll)
}

func (c Config) RequestTimeoutDuration() (time.Duration, error) {
	return parseDuration("request_timeout", c.RequestTimeout)
}

func (c Config) ReadyTimeoutDuration() (time.Duration, error) {
	return parseDuration("ready_timeout", c.ReadyTimeout)
}

func parseDuration(name string, value string) (time.Duration, error) {
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	return d, nil
}

func newViper(path string) *viper.Viper {
	cfg := Defaults()
	v := viper.New()
	v.SetConfigFile(configPath(path))
	v.SetConfigType("json")

	v.SetDefault("state_dir", cfg.StateDir)
	v.SetDefault("default_session", cfg.DefaultSession)
	v.SetDefault("tmux_prefix", cfg.TmuxPrefix)
	v.SetDefault("claude_binary", cfg.ClaudeBinary)
	v.SetDefault("claude_args", cfg.ClaudeArgs)
	v.SetDefault("startup_wait", cfg.StartupWait)
	v.SetDefault("wait_timeout", cfg.WaitTimeout)
	v.SetDefault("wait_poll_interval", cfg.WaitPollInterval)
	v.SetDefault("capture_delay", cfg.CaptureDelay)
	v.SetDefault("capture_lines", cfg.CaptureLines)
	v.SetDefault("daemon_poll", cfg.DaemonPoll)
	v.SetDefault("request_timeout", cfg.RequestTimeout)
	v.SetDefault("ready_marker", cfg.ReadyMarker)
	v.SetDefault("ready_timeout", cfg.ReadyTimeout)

	return v
}

func configPath(path string) string {
	if path == "" {
		return DefaultPath()
	}
	return path
}
