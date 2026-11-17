// Package config loads the project configuration (sdd.yaml) and resolves
// environment values from the process environment and an optional .env file.
//
// Precedence for every value that can come from the environment:
// process environment > .env file > sdd.yaml > built-in defaults.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/darkcarard/quipu-sdd/internal/version"
)

// FileName is the project configuration file at the repository root.
const FileName = "sdd.yaml"

// DotEnvFile is the optional, git-ignored file holding secrets such as API keys.
const DotEnvFile = ".env"

// Environment variables that override sdd.yaml.
const (
	EnvProvider = "SDD_PROVIDER"
	EnvModel    = "SDD_MODEL"
)

// Config is the content of sdd.yaml.
type Config struct {
	FrameworkVersion int         `yaml:"framework_version"`
	Provider         Provider    `yaml:"provider"`
	Limits           Limits      `yaml:"limits"`
	MCPServers       []MCPServer `yaml:"mcp_servers"`
	Audit            Audit       `yaml:"audit"`

	// env is the merged view of .env and the process environment (not serialized).
	env map[string]string
}

// Provider selects the LLM backend.
type Provider struct {
	Name      string `yaml:"name"`        // anthropic | gemini | openai
	Model     string `yaml:"model"`       // provider-specific model id
	APIKeyEnv string `yaml:"api_key_env"` // name of the env var holding the key (never the key itself)
	MaxTokens int    `yaml:"max_tokens"`  // max output tokens per request (thinking included)
	// Effort trades thoroughness for tokens: low | medium | high | xhigh | max.
	Effort string `yaml:"effort"`
	// ThinkingDisplay controls what reasoning comes back: omitted | summarized | updates.
	ThinkingDisplay string `yaml:"thinking_display"`
	// Fallbacks lets the provider re-serve a declined request with a fallback model.
	Fallbacks bool `yaml:"fallbacks"`
	// BaseURL overrides the provider endpoint (e.g. a proxy); empty uses the official one.
	BaseURL string `yaml:"base_url"`
}

// Limits bound the agent loops. See docs/BRIEF.md §4.
type Limits struct {
	MaxIterationsPerTask int           `yaml:"max_iterations_per_task"`
	MaxFixAttempts       int           `yaml:"max_fix_attempts"`
	MaxFilesPerTask      int           `yaml:"max_files_per_task"`
	MaxRetries           int           `yaml:"max_retries"`
	RequestTimeout       time.Duration `yaml:"request_timeout"`
	CommandTimeout       time.Duration `yaml:"command_timeout"`
	// OutputTail is how many characters of a failing command's output are
	// returned to the agent and kept in the review (0: the default, 6000).
	OutputTail  int `yaml:"output_tail"`
	TokenBudget int `yaml:"token_budget"` // 0 = unlimited
}

// MCPServer is an MCP server spawned over stdio. Only tools listed in AllowTools
// are exposed to the agent; an empty list exposes none.
type MCPServer struct {
	Name       string            `yaml:"name"`
	Command    string            `yaml:"command"`
	Args       []string          `yaml:"args"`
	Env        map[string]string `yaml:"env"`
	AllowTools []string          `yaml:"allow_tools"`
}

// Audit controls what is persisted for traceability.
type Audit struct {
	CommitTranscripts bool `yaml:"commit_transcripts"`
	// RedactPatterns are extra regular expressions redacted from transcripts,
	// in addition to the built-in credential patterns and the configured secrets.
	RedactPatterns []string `yaml:"redact_patterns"`
}

// Default returns the built-in configuration.
//
// Provider, MCPServers, Audit and the API-mode limits configure the parked API
// mode (an agent loop inside sdd); they are not written to a new sdd.yaml and
// nothing in the CLI uses them.
func Default() Config {
	return Config{
		FrameworkVersion: version.LayoutVersion,
		Limits: Limits{
			MaxFixAttempts:  3,
			MaxFilesPerTask: 5,
			CommandTimeout:  10 * time.Minute,
			OutputTail:      6000,
		},
	}
}

// DefaultYAML is the commented sdd.yaml written by `sdd init`. It must decode to Default().
func DefaultYAML() string {
	return fmt.Sprintf(`# SDD framework project configuration. See docs/BRIEF.md.
# sdd never calls an LLM: the coding agent you run it with does the reasoning.
framework_version: %d

limits:
  max_files_per_task: 5       # plan validation: keeps tasks reviewable
  max_fix_attempts: 3         # group verification failures before the group is marked failed
  command_timeout: 10m        # build, lint and test commands are killed after this long
  output_tail: 6000           # characters of a failing command's output returned to the agent
`, version.LayoutVersion)
}

// outsideWorktree maps a feature worktree (<root>/.sdd/worktrees/<feature>, which
// holds its own checked-out copy of sdd.yaml) to the project that owns it.
func outsideWorktree(d string) string {
	worktrees := filepath.Dir(d)
	if filepath.Base(worktrees) != "worktrees" || filepath.Base(filepath.Dir(worktrees)) != ".sdd" {
		return d
	}
	root := filepath.Dir(filepath.Dir(worktrees))
	if _, err := os.Stat(filepath.Join(root, FileName)); err == nil {
		return root
	}
	return d
}

// FindRoot returns the nearest directory at or above dir that contains sdd.yaml.
// Inside a feature worktree it returns the project that owns the worktree.
func FindRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for d := abs; ; {
		if _, err := os.Stat(filepath.Join(d, FileName)); err == nil {
			return outsideWorktree(d), nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", fmt.Errorf("no %s in %s or any parent directory: run `sdd init` first", FileName, abs)
		}
		d = parent
	}
}

// Load reads <root>/sdd.yaml (missing fields keep their defaults), merges
// <root>/.env with the process environment and applies environment overrides.
func Load(root string) (Config, error) {
	cfg := Default()
	b, err := os.ReadFile(filepath.Join(root, FileName))
	if err != nil {
		return cfg, fmt.Errorf("reading %s (run `sdd init` first?): %w", FileName, err)
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing %s: %w", FileName, err)
	}
	if cfg.FrameworkVersion > version.LayoutVersion {
		return cfg, fmt.Errorf("%s requires framework layout v%d but this binary supports v%d: upgrade sdd",
			FileName, cfg.FrameworkVersion, version.LayoutVersion)
	}

	env, err := loadEnv(root)
	if err != nil {
		return cfg, err
	}
	cfg.env = env
	if v := env[EnvProvider]; v != "" {
		cfg.Provider.Name = v
	}
	if v := env[EnvModel]; v != "" {
		cfg.Provider.Model = v
	}
	return cfg, cfg.Validate()
}

// Validate checks values that would otherwise fail later in confusing ways.
func (c Config) Validate() error {
	var errs []error
	if c.Provider.Name != "" { // parked API mode: checked only when configured
		switch c.Provider.Name {
		case "anthropic", "gemini", "openai":
		default:
			errs = append(errs, fmt.Errorf("provider.name: unsupported provider %q", c.Provider.Name))
		}
		switch c.Provider.Effort {
		case "", "low", "medium", "high", "xhigh", "max":
		default:
			errs = append(errs, fmt.Errorf("provider.effort: %q is not one of low, medium, high, xhigh, max", c.Provider.Effort))
		}
		switch c.Provider.ThinkingDisplay {
		case "", "omitted", "summarized", "updates":
		default:
			errs = append(errs, fmt.Errorf("provider.thinking_display: %q is not one of omitted, summarized, updates", c.Provider.ThinkingDisplay))
		}
		if c.Provider.MaxTokens < 0 {
			errs = append(errs, errors.New("provider.max_tokens: must be >= 0"))
		}
	}
	for i, p := range c.Audit.RedactPatterns {
		if _, err := regexp.Compile(p); err != nil {
			errs = append(errs, fmt.Errorf("audit.redact_patterns[%d]: %w", i, err))
		}
	}
	if c.Limits.OutputTail < 0 {
		errs = append(errs, errors.New("limits.output_tail: must be >= 0"))
	}
	if c.Limits.MaxFixAttempts < 0 {
		errs = append(errs, errors.New("limits.max_fix_attempts: must be >= 0"))
	}
	if c.Limits.MaxFilesPerTask < 1 {
		errs = append(errs, errors.New("limits.max_files_per_task: must be >= 1"))
	}
	for i, s := range c.MCPServers {
		if s.Name == "" || s.Command == "" {
			errs = append(errs, fmt.Errorf("mcp_servers[%d]: name and command are required", i))
		}
	}
	return errors.Join(errs...)
}

// Lookup returns an environment value with precedence process env > .env.
func (c Config) Lookup(key string) (string, bool) {
	if v, ok := os.LookupEnv(key); ok {
		return v, true
	}
	v, ok := c.env[key]
	return v, ok
}

// APIKey returns the provider API key from the environment or .env.
func (c Config) APIKey() (string, error) {
	if v, ok := c.Lookup(c.Provider.APIKeyEnv); ok && v != "" {
		return v, nil
	}
	return "", fmt.Errorf("%s is not set: export it or add it to %s", c.Provider.APIKeyEnv, DotEnvFile)
}

// secretNameRe matches environment variable names that hold credentials.
var secretNameRe = regexp.MustCompile(`(?i)(KEY|TOKEN|SECRET|PASSWORD|PASSWD|CREDENTIAL|AUTH)`)

// Secrets returns the values to redact from transcripts and logs: the provider
// API key and every environment value (process or .env) whose name looks like
// a credential.
func (c Config) Secrets() []string {
	var out []string
	if v, ok := c.Lookup(c.Provider.APIKeyEnv); ok && v != "" {
		out = append(out, v)
	}
	keys := make([]string, 0, len(c.env))
	for k := range c.env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if v := c.env[k]; v != "" && secretNameRe.MatchString(k) {
			out = append(out, v)
		}
	}
	return out
}

// loadEnv merges <root>/.env with the process environment (process wins).
func loadEnv(root string) (map[string]string, error) {
	env := map[string]string{}
	b, err := os.ReadFile(filepath.Join(root, DotEnvFile))
	switch {
	case err == nil:
		if env, err = ParseDotEnv(string(b)); err != nil {
			return nil, fmt.Errorf("%s: %w", DotEnvFile, err)
		}
	case !errors.Is(err, fs.ErrNotExist):
		return nil, err
	}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	return env, nil
}

// ParseDotEnv parses KEY=VALUE lines. It supports blank lines, # comments, an
// optional `export ` prefix, single/double-quoted values and trailing ` #` comments
// on unquoted values. Variable expansion is intentionally not supported.
func ParseDotEnv(content string) (map[string]string, error) {
	out := map[string]string{}
	for i, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" || strings.ContainsAny(key, " \t") {
			return nil, fmt.Errorf("line %d: expected KEY=VALUE", i+1)
		}
		val = strings.TrimSpace(val)
		switch {
		case len(val) >= 2 && (val[0] == '"' || val[0] == '\''):
			end := strings.IndexByte(val[1:], val[0])
			if end < 0 {
				return nil, fmt.Errorf("line %d: unterminated quote", i+1)
			}
			val = val[1 : end+1]
		default:
			if idx := strings.Index(val, " #"); idx >= 0 {
				val = strings.TrimSpace(val[:idx])
			}
		}
		out[key] = val
	}
	return out, nil
}
