package config

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func newFS() *flag.FlagSet {
	fs := flag.NewFlagSet("snp", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.String("config", "", "")
	fs.String("hostname", "", "")
	fs.String("owner", "", "")
	fs.String("state-dir", "", "")
	fs.String("log-level", "", "")
	fs.String("dev-listen", "", "")
	fs.String("ai-endpoint", "", "")
	fs.String("ai-model", "", "")
	fs.String("ai-key", "", "")
	return fs
}

// isolateHome points HOME and XDG_CONFIG_HOME at empty temp dirs so
// tests never read the developer's real config.
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	return home
}

func writeConfig(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "config.toml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDefaults(t *testing.T) {
	home := isolateHome(t)
	fs := newFS()
	fs.Set("owner", "alice@github")
	c, err := Load(fs)
	if err != nil {
		t.Fatal(err)
	}
	if c.Hostname != "snp" {
		t.Errorf("hostname = %q, want snp", c.Hostname)
	}
	if want := filepath.Join(home, ".local", "share", "snp"); c.StateDir != want {
		t.Errorf("state_dir = %q, want %q", c.StateDir, want)
	}
	if c.LogLevel != "info" {
		t.Errorf("log_level = %q", c.LogLevel)
	}
}

func TestFileValues(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, filepath.Join(home, ".config", "snp"),
		"hostname = \"h1\"\nowner = \"o1\"\nstate_dir = \"/tmp/snp-state\"\nlog_level = \"debug\"\n")
	fs := newFS()
	c, err := Load(fs)
	if err != nil {
		t.Fatal(err)
	}
	if c.Hostname != "h1" || c.Owner != "o1" || c.StateDir != "/tmp/snp-state" || c.LogLevel != "debug" {
		t.Errorf("got %+v", c)
	}
}

func TestEnvBeatsFile(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, filepath.Join(home, ".config", "snp"), "hostname = \"h1\"\nowner = \"o1\"\n")
	t.Setenv("SNP_HOSTNAME", "h2")
	fs := newFS()
	c, err := Load(fs)
	if err != nil {
		t.Fatal(err)
	}
	if c.Hostname != "h2" {
		t.Errorf("hostname = %q, want h2", c.Hostname)
	}
	if c.Owner != "o1" {
		t.Errorf("owner = %q, want o1 (from file)", c.Owner)
	}
}

func TestFlagBeatsEnv(t *testing.T) {
	isolateHome(t)
	t.Setenv("SNP_HOSTNAME", "h2")
	t.Setenv("SNP_OWNER", "o@example.com")
	fs := newFS()
	fs.Set("hostname", "h3")
	c, err := Load(fs)
	if err != nil {
		t.Fatal(err)
	}
	if c.Hostname != "h3" {
		t.Errorf("hostname = %q, want h3", c.Hostname)
	}
}

func TestConfigFlag(t *testing.T) {
	isolateHome(t)
	p := writeConfig(t, t.TempDir(), "hostname = \"custom\"\nowner = \"o1\"\n")
	fs := newFS()
	fs.Set("config", p)
	c, err := Load(fs)
	if err != nil {
		t.Fatal(err)
	}
	if c.Hostname != "custom" {
		t.Errorf("hostname = %q, want custom", c.Hostname)
	}
}

func TestConfigFlagMissingFile(t *testing.T) {
	isolateHome(t)
	fs := newFS()
	fs.Set("owner", "o1")
	fs.Set("config", "/nonexistent/snp-config.toml")
	if _, err := Load(fs); err == nil {
		t.Error("expected error for missing --config file")
	}
}

func TestMalformedTOML(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, filepath.Join(home, ".config", "snp"), "hostname = \n")
	fs := newFS()
	fs.Set("owner", "o1")
	if _, err := Load(fs); err == nil {
		t.Error("expected error for malformed TOML")
	}
}

func TestMissingOwner(t *testing.T) {
	isolateHome(t)
	fs := newFS()
	if _, err := Load(fs); err == nil {
		t.Error("expected error for missing owner")
	}
}

func TestDevModeWithoutOwner(t *testing.T) {
	isolateHome(t)
	fs := newFS()
	fs.Set("dev-listen", "127.0.0.1:8080")
	c, err := Load(fs)
	if err != nil {
		t.Fatal(err)
	}
	if c.DevListen != "127.0.0.1:8080" {
		t.Errorf("dev_listen = %q", c.DevListen)
	}
}

// TestDesktopWithoutOwner: the desktop app has no HTTP surface, so it
// must load without an owner and without --dev-listen (spec §12).
func TestDesktopWithoutOwner(t *testing.T) {
	home := isolateHome(t)
	fs := newFS()
	c, err := LoadDesktop(fs)
	if err != nil {
		t.Fatal(err)
	}
	if c.Owner != "" {
		t.Errorf("owner = %q, want empty", c.Owner)
	}
	if c.DevListen != "" {
		t.Errorf("dev-listen = %q, want empty", c.DevListen)
	}
	if want := filepath.Join(home, ".local", "share", "snp"); c.StateDir != want {
		t.Errorf("state-dir = %q, want %q", c.StateDir, want)
	}
	// File resolution still applies to a desktop config.
	p := writeConfig(t, filepath.Join(home, ".config", "snp"), "state_dir = \"~/dt-state\"\n")
	fs2 := newFS()
	fs2.Set("config", p)
	c2, err := LoadDesktop(fs2)
	if err != nil {
		t.Fatal(err)
	}
	if c2.Owner != "" {
		t.Errorf("desktop owner = %q, want empty", c2.Owner)
	}
	if want := filepath.Join(home, "dt-state"); c2.StateDir != want {
		t.Errorf("desktop state-dir = %q, want %q", c2.StateDir, want)
	}
	// The owner requirement still holds for Load proper.
	if _, err := Load(fs); err == nil {
		t.Error("Load without owner should still fail")
	}
}

func TestHomeExpansion(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, filepath.Join(home, ".config", "snp"), "owner = \"o1\"\nstate_dir = \"~/snp-state\"\n")
	fs := newFS()
	c, err := Load(fs)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, "snp-state"); c.StateDir != want {
		t.Errorf("state_dir = %q, want %q", c.StateDir, want)
	}
}

func TestInvalidLogLevel(t *testing.T) {
	isolateHome(t)
	fs := newFS()
	fs.Set("owner", "o1")
	fs.Set("log-level", "verbose")
	if _, err := Load(fs); err == nil {
		t.Error("expected error for invalid log level")
	}
}

func TestAIFields(t *testing.T) {
	home := isolateHome(t)
	writeConfig(t, filepath.Join(home, ".config", "snp"), `owner = "o1"
ai_endpoint = "http://127.0.0.1:9000/v1"
ai_model = "my-model"
ai_key = "secret"
`)
	fs := newFS()
	c, err := Load(fs)
	if err != nil {
		t.Fatal(err)
	}
	if c.AIEndpoint != "http://127.0.0.1:9000/v1" || c.AIModel != "my-model" || c.AIKey != "secret" {
		t.Errorf("file values = %+v", c)
	}
	if !c.AIEnabled() {
		t.Error("AI should be enabled with a key")
	}
	// Env beats file; flag beats env.
	t.Setenv("SNP_AI_ENDPOINT", "http://env:1/v1")
	t.Setenv("SNP_AI_MODEL", "env-model")
	t.Setenv("SNP_AI_KEY", "env-secret")
	fs2 := newFS()
	c2, err := Load(fs2)
	if err != nil {
		t.Fatal(err)
	}
	if c2.AIEndpoint != "http://env:1/v1" || c2.AIModel != "env-model" || c2.AIKey != "env-secret" {
		t.Errorf("env values = %+v", c2)
	}
	fs3 := newFS()
	fs3.Set("ai-endpoint", "http://flag:1/v1")
	fs3.Set("ai-key", "flag-secret")
	c3, err := Load(fs3)
	if err != nil {
		t.Fatal(err)
	}
	if c3.AIEndpoint != "http://flag:1/v1" || c3.AIKey != "flag-secret" {
		t.Errorf("flag values = %+v", c3)
	}
	// Defaults without a key: disabled, openai defaults.
}

func TestAIDefaults(t *testing.T) {
	isolateHome(t)
	t.Setenv("SNP_AI_ENDPOINT", "")
	t.Setenv("SNP_AI_MODEL", "")
	t.Setenv("SNP_AI_KEY", "")
	fs := newFS()
	fs.Set("owner", "o1")
	c, err := Load(fs)
	if err != nil {
		t.Fatal(err)
	}
	if c.AIEnabled() {
		t.Error("AI should be disabled without a key")
	}
	if c.AIEndpoint != "https://api.openai.com/v1" || c.AIModel != "gpt-4o-mini" {
		t.Errorf("defaults = %+v", c)
	}
}
