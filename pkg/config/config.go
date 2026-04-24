package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	AgentName       string                    `json:"agent_name"`
	DefaultProvider string                    `json:"default_provider"`
	Providers       map[string]ProviderConfig `json:"providers"`
	Repo            RepoConfig                `json:"repo"`
	Policy          PolicyConfig              `json:"policy"`
	Limits          LimitsConfig              `json:"limits"`
	DataDir         string                    `json:"data_dir"`
	HTTP            HTTPConfig                `json:"http"`
}

type ProviderConfig struct {
	Type      string `json:"type,omitempty"`
	BaseURL   string `json:"base_url"`
	Model     string `json:"model"`
	APIKey    string `json:"api_key,omitempty"`
	APIKeyEnv string `json:"api_key_env,omitempty"`
}

type RepoConfig struct {
	Root          string `json:"root"`
	WorkspaceRoot string `json:"workspace_root"`
}

type PolicyConfig struct {
	RequireApprovalFor []string `json:"require_approval_for"`
}

type LimitsConfig struct {
	MaxConcurrentTasks  int   `json:"max_concurrent_tasks"`
	MaxToolCallsPerTurn int   `json:"max_tool_calls_per_turn"`
	MaxShellSeconds     int   `json:"max_shell_seconds"`
	MaxFileBytes        int64 `json:"max_file_bytes"`
}

type HTTPConfig struct {
	Addr string `json:"addr"`
}

func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		return cfg.WithDefaults(), nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg.WithDefaults(), nil
		}
		return Config{}, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Config{}, err
	}
	return cfg.WithDefaults(), nil
}

func LoadDotEnv(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		if key != "" && os.Getenv(key) == "" {
			if err := os.Setenv(key, value); err != nil {
				return err
			}
		}
	}
	return nil
}

func Default() Config {
	wd, _ := os.Getwd()
	return Config{
		AgentName:       "homelab-lead",
		DefaultProvider: "gemini",
		Providers: map[string]ProviderConfig{
			"gemini": {Type: "gemini", BaseURL: "https://generativelanguage.googleapis.com/v1beta", Model: "gemini-flash-latest", APIKeyEnv: "GEMINI_API_KEY"},
			"openai": {Type: "openai-compatible", BaseURL: "https://api.openai.com/v1", Model: "gpt-5.1", APIKeyEnv: "OPENAI_API_KEY"},
			"ollama": {Type: "openai-compatible", BaseURL: "http://localhost:11434/v1", Model: "qwen3-coder"},
		},
		Repo: RepoConfig{
			Root:          wd,
			WorkspaceRoot: filepath.Join(wd, "workspaces"),
		},
		Policy: PolicyConfig{
			RequireApprovalFor: []string{
				"shell.run_approved",
				"service.restart",
				"llm.switch_provider",
				"git.commit",
				"repo.apply_patch_to_main",
				"git.merge_approved",
			},
		},
		Limits: LimitsConfig{
			MaxConcurrentTasks:  4,
			MaxToolCallsPerTurn: 12,
			MaxShellSeconds:     60,
			MaxFileBytes:        1 << 20,
		},
		DataDir: "data",
		HTTP:    HTTPConfig{Addr: "127.0.0.1:8080"},
	}
}

func (c Config) WithDefaults() Config {
	d := Default()
	if c.AgentName == "" {
		c.AgentName = d.AgentName
	}
	if c.DefaultProvider == "" {
		c.DefaultProvider = d.DefaultProvider
	}
	if c.Providers == nil {
		c.Providers = d.Providers
	} else {
		for name, provider := range d.Providers {
			if _, ok := c.Providers[name]; !ok {
				c.Providers[name] = provider
			}
		}
	}
	for name, provider := range c.Providers {
		if provider.Type == "" {
			provider.Type = d.Providers[name].Type
		}
		if provider.APIKey == "" && provider.APIKeyEnv != "" {
			provider.APIKey = os.Getenv(provider.APIKeyEnv)
		}
		c.Providers[name] = provider
	}
	if c.Repo.Root == "" {
		c.Repo.Root = d.Repo.Root
	}
	if c.Repo.WorkspaceRoot == "" {
		c.Repo.WorkspaceRoot = d.Repo.WorkspaceRoot
	}
	if c.Policy.RequireApprovalFor == nil {
		c.Policy.RequireApprovalFor = d.Policy.RequireApprovalFor
	} else {
		seen := make(map[string]bool, len(c.Policy.RequireApprovalFor))
		for _, name := range c.Policy.RequireApprovalFor {
			seen[name] = true
		}
		for _, name := range d.Policy.RequireApprovalFor {
			if !seen[name] {
				c.Policy.RequireApprovalFor = append(c.Policy.RequireApprovalFor, name)
			}
		}
	}
	if c.Limits.MaxConcurrentTasks == 0 {
		c.Limits.MaxConcurrentTasks = d.Limits.MaxConcurrentTasks
	}
	if c.Limits.MaxToolCallsPerTurn == 0 {
		c.Limits.MaxToolCallsPerTurn = d.Limits.MaxToolCallsPerTurn
	}
	if c.Limits.MaxShellSeconds == 0 {
		c.Limits.MaxShellSeconds = d.Limits.MaxShellSeconds
	}
	if c.Limits.MaxFileBytes == 0 {
		c.Limits.MaxFileBytes = d.Limits.MaxFileBytes
	}
	if c.DataDir == "" {
		c.DataDir = d.DataDir
	}
	if c.HTTP.Addr == "" {
		c.HTTP.Addr = d.HTTP.Addr
	}
	return c
}
