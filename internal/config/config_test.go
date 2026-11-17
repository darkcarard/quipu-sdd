package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDefaultYAMLMatchesDefault(t *testing.T) {
	cfg := Default()
	var got Config
	if err := yaml.Unmarshal([]byte(DefaultYAML()), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, cfg) {
		t.Fatalf("DefaultYAML() decodes to\n%+v\nwant\n%+v", got, cfg)
	}
}

func TestParseDotEnv(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    map[string]string
		wantErr bool
	}{
		{"basic", "A=1\nB=two", map[string]string{"A": "1", "B": "two"}, false},
		{"comments and blanks", "# c\n\nA=1 # trailing\n", map[string]string{"A": "1"}, false},
		{"export prefix", "export A=1", map[string]string{"A": "1"}, false},
		{"double quotes keep hash", `A="x # y"`, map[string]string{"A": "x # y"}, false},
		{"single quotes", "A='v=1'", map[string]string{"A": "v=1"}, false},
		{"empty value", "A=", map[string]string{"A": ""}, false},
		{"crlf", "A=1\r\nB=2\r\n", map[string]string{"A": "1", "B": "2"}, false},
		{"missing equals", "NOPE", nil, true},
		{"unterminated quote", `A="x`, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDotEnv(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func writeProject(t *testing.T, yamlContent, dotenv string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if dotenv != "" {
		if err := os.WriteFile(filepath.Join(dir, DotEnvFile), []byte(dotenv), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLoadPrecedence(t *testing.T) {
	yml := "provider:\n  name: anthropic\n  model: from-yaml\n  api_key_env: TEST_SDD_KEY\n"

	t.Run("yaml only", func(t *testing.T) {
		t.Setenv(EnvModel, "")
		os.Unsetenv(EnvModel)
		cfg, err := Load(writeProject(t, yml, ""))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Provider.Model != "from-yaml" {
			t.Fatalf("model = %q", cfg.Provider.Model)
		}
		if cfg.Limits.MaxFilesPerTask != 5 {
			t.Fatalf("missing fields should keep defaults, got %d", cfg.Limits.MaxFilesPerTask)
		}
	})

	t.Run(".env beats yaml", func(t *testing.T) {
		t.Setenv(EnvModel, "")
		os.Unsetenv(EnvModel)
		cfg, err := Load(writeProject(t, yml, "SDD_MODEL=from-dotenv\nTEST_SDD_KEY=secret-dotenv\n"))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Provider.Model != "from-dotenv" {
			t.Fatalf("model = %q", cfg.Provider.Model)
		}
		if key, err := cfg.APIKey(); err != nil || key != "secret-dotenv" {
			t.Fatalf("APIKey = %q, %v", key, err)
		}
	})

	t.Run("process env beats .env", func(t *testing.T) {
		t.Setenv(EnvModel, "from-env")
		t.Setenv("TEST_SDD_KEY", "secret-env")
		cfg, err := Load(writeProject(t, yml, "SDD_MODEL=from-dotenv\nTEST_SDD_KEY=secret-dotenv\n"))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Provider.Model != "from-env" {
			t.Fatalf("model = %q", cfg.Provider.Model)
		}
		if key, _ := cfg.APIKey(); key != "secret-env" {
			t.Fatalf("APIKey = %q", key)
		}
	})
}

func TestLoadErrors(t *testing.T) {
	if _, err := Load(t.TempDir()); err == nil || !strings.Contains(err.Error(), "sdd init") {
		t.Fatalf("missing sdd.yaml should hint at sdd init, got %v", err)
	}
	if _, err := Load(writeProject(t, "framework_version: 999\n", "")); err == nil {
		t.Fatal("newer layout version should be rejected")
	}
	if _, err := Load(writeProject(t, "provider:\n  name: ollama\n", "")); err == nil {
		t.Fatal("unsupported provider should be rejected")
	}
}

func TestAPIKeyMissing(t *testing.T) {
	cfg := Default()
	cfg.Provider.APIKeyEnv = "TEST_SDD_DEFINITELY_UNSET"
	if _, err := cfg.APIKey(); err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestSecretsAndProviderValidation(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "sdd.yaml"), []byte("provider:\n  name: anthropic\n  model: m\n  api_key_env: SDD_T_KEY\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".env"), []byte("SDD_T_KEY=sk-ant-from-dotenv\nSDD_T_DB_PASSWORD=pw-1234567\nSDD_T_REGION=us-east-1\n"), 0o644)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	secrets := strings.Join(cfg.Secrets(), "|")
	if !strings.Contains(secrets, "sk-ant-from-dotenv") || !strings.Contains(secrets, "pw-1234567") || strings.Contains(secrets, "us-east-1") {
		t.Errorf("secrets = %q", secrets)
	}

	for field, yaml := range map[string]string{
		"provider.effort":           "provider:\n  name: anthropic\n  model: m\n  effort: extreme\n",
		"provider.thinking_display": "provider:\n  name: anthropic\n  model: m\n  thinking_display: full\n",
		"provider.max_tokens":       "provider:\n  name: anthropic\n  model: m\n  max_tokens: -1\n",
		"audit.redact_patterns":     "audit:\n  redact_patterns: [\"(\"]\n",
	} {
		d := t.TempDir()
		os.WriteFile(filepath.Join(d, "sdd.yaml"), []byte(yaml), 0o644)
		if _, err := Load(d); err == nil || !strings.Contains(err.Error(), field) {
			t.Errorf("%s: err = %v", field, err)
		}
	}
}

func TestFindRoot(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "a", "b")
	os.MkdirAll(sub, 0o755)
	if _, err := FindRoot(sub); err == nil || !strings.Contains(err.Error(), "run `sdd init` first") {
		t.Errorf("no sdd.yaml: %v", err)
	}
	os.WriteFile(filepath.Join(root, FileName), []byte(DefaultYAML()), 0o644)
	got, err := FindRoot(sub)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(root)
	if g, _ := filepath.EvalSymlinks(got); g != want {
		t.Errorf("FindRoot = %s, want %s", got, root)
	}
}
