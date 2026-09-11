// Package config resolves snp configuration from flags, environment,
// and a TOML file. Precedence per key: flag > env > file > default
// (spec §2).
package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Config is the fully resolved configuration.
type Config struct {
	// Hostname is the tailnet node name.
	Hostname string
	// Owner is the Tailscale login allowed in. Required unless
	// DevListen is set.
	Owner string
	// StateDir holds tsnet state, the database, and the key.
	StateDir string
	// LogLevel is one of debug, info, warn, error.
	LogLevel string
	// DevListen, when non-empty, enables dev mode: plain HTTP on the
	// address, no tsnet, no auth (spec §1).
	DevListen string

	// AIEndpoint is the base URL of an OpenAI-compatible chat
	// completions API (spec §13). Empty means the OpenAI default.
	AIEndpoint string
	// AIModel is the model name sent to the endpoint.
	AIModel string
	// AIKey is the bearer API key. The AI feature is enabled when the
	// key is set; the key is never exposed over the API.
	AIKey string
}

// AIEnabled reports whether the AI feature is configured.
func (c Config) AIEnabled() bool { return c.AIKey != "" }

// Environment variable names.
const (
	EnvConfig     = "SNP_CONFIG"
	EnvHostname   = "SNP_HOSTNAME"
	EnvOwner      = "SNP_OWNER"
	EnvStateDir   = "SNP_STATE_DIR"
	EnvLogLevel   = "SNP_LOG_LEVEL"
	EnvAIEndpoint = "SNP_AI_ENDPOINT"
	EnvAIModel    = "SNP_AI_MODEL"
	EnvAIKey      = "SNP_AI_KEY"
)

// Defaults returns the built-in default configuration.
func Defaults() Config {
	return Config{
		Hostname:   "snp",
		StateDir:   "~/.local/share/snp",
		LogLevel:   "info",
		AIEndpoint: "https://api.openai.com/v1",
		AIModel:    "gpt-4o-mini",
	}
}

type fileConfig struct {
	Hostname   string `toml:"hostname"`
	Owner      string `toml:"owner"`
	StateDir   string `toml:"state_dir"`
	LogLevel   string `toml:"log_level"`
	AIEndpoint string `toml:"ai_endpoint"`
	AIModel    string `toml:"ai_model"`
	AIKey      string `toml:"ai_key"`
}

// Load resolves the configuration. fs must contain the string flags
// config, hostname, owner, state-dir, log-level, and dev-listen.
func Load(fs *flag.FlagSet) (Config, error) {
	return load(fs, true)
}

// LoadDesktop resolves the configuration for the desktop app
// (cmd/snp-desktop). It is Load without the owner-required rule: a
// desktop instance is local and single-user (spec §12) and exposes no
// HTTP surface to authenticate. fs must contain the same flags as
// Load (dev-listen is ignored).
func LoadDesktop(fs *flag.FlagSet) (Config, error) {
	return load(fs, false)
}

// load applies file → env → flag resolution and validates the result.
func load(fs *flag.FlagSet, requireOwner bool) (Config, error) {
	c := Defaults()
	set := setFlagNames(fs)

	// 1. TOML file.
	path, err := findFile(fs)
	if err != nil {
		return c, err
	}
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return c, fmt.Errorf("config: read %s: %w", path, err)
		}
		var f fileConfig
		if err := toml.Unmarshal(data, &f); err != nil {
			return c, fmt.Errorf("config: parse %s: %w", path, err)
		}
		if f.Hostname != "" {
			c.Hostname = f.Hostname
		}
		if f.Owner != "" {
			c.Owner = f.Owner
		}
		if f.StateDir != "" {
			c.StateDir = f.StateDir
		}
		if f.LogLevel != "" {
			c.LogLevel = f.LogLevel
		}
		if f.AIEndpoint != "" {
			c.AIEndpoint = f.AIEndpoint
		}
		if f.AIModel != "" {
			c.AIModel = f.AIModel
		}
		if f.AIKey != "" {
			c.AIKey = f.AIKey
		}
	}

	// 2. Environment.
	if v := os.Getenv(EnvHostname); v != "" {
		c.Hostname = v
	}
	if v := os.Getenv(EnvOwner); v != "" {
		c.Owner = v
	}
	if v := os.Getenv(EnvStateDir); v != "" {
		c.StateDir = v
	}
	if v := os.Getenv(EnvLogLevel); v != "" {
		c.LogLevel = v
	}
	if v := os.Getenv(EnvAIEndpoint); v != "" {
		c.AIEndpoint = v
	}
	if v := os.Getenv(EnvAIModel); v != "" {
		c.AIModel = v
	}
	if v := os.Getenv(EnvAIKey); v != "" {
		c.AIKey = v
	}

	// 3. Flags (only if explicitly set).
	if v := flagValue(fs, set, "hostname"); v != "" {
		c.Hostname = v
	}
	if v := flagValue(fs, set, "owner"); v != "" {
		c.Owner = v
	}
	if v := flagValue(fs, set, "state-dir"); v != "" {
		c.StateDir = v
	}
	if v := flagValue(fs, set, "log-level"); v != "" {
		c.LogLevel = v
	}
	if v := flagValue(fs, set, "dev-listen"); v != "" {
		c.DevListen = v
	}
	if v := flagValue(fs, set, "ai-endpoint"); v != "" {
		c.AIEndpoint = v
	}
	if v := flagValue(fs, set, "ai-model"); v != "" {
		c.AIModel = v
	}
	if v := flagValue(fs, set, "ai-key"); v != "" {
		c.AIKey = v
	}

	// Normalize and validate.
	c.StateDir, err = expandPath(c.StateDir, homeDir())
	if err != nil {
		return c, fmt.Errorf("config: state-dir: %w", err)
	}
	if c.Hostname == "" {
		return c, fmt.Errorf("config: hostname must not be empty")
	}
	if requireOwner && c.Owner == "" && c.DevListen == "" {
		return c, fmt.Errorf("config: owner is required (or use --dev-listen)")
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return c, fmt.Errorf("config: log-level %q must be one of debug, info, warn, error", c.LogLevel)
	}
	return c, nil
}

// setFlagNames returns the names of the flags explicitly set on the
// command line.
func setFlagNames(fs *flag.FlagSet) map[string]bool {
	out := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { out[f.Name] = true })
	return out
}

// flagValue returns the value of a string flag if it was set on the
// command line, otherwise "".
func flagValue(fs *flag.FlagSet, set map[string]bool, name string) string {
	if !set[name] {
		return ""
	}
	if f := fs.Lookup(name); f != nil {
		return f.Value.String()
	}
	return ""
}

// findFile resolves the config file path: --config > $SNP_CONFIG >
// $XDG_CONFIG_HOME/snp/config.toml > ~/.config/snp/config.toml
// (spec §2). Missing discovered files are skipped; an explicit
// --config that does not exist is an error at read time.
func findFile(fs *flag.FlagSet) (string, error) {
	set := setFlagNames(fs)
	if p := flagValue(fs, set, "config"); p != "" {
		return expandPath(p, homeDir())
	}
	if p := os.Getenv(EnvConfig); p != "" {
		return expandPath(p, homeDir())
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		if p := filepath.Join(xdg, "snp", "config.toml"); exists(p) {
			return p, nil
		}
	}
	if p := filepath.Join(homeDir(), ".config", "snp", "config.toml"); exists(p) {
		return p, nil
	}
	return "", nil
}

func expandPath(p, home string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home == "" {
			return "", fmt.Errorf("cannot expand %q: no home directory", p)
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return abs, nil
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
