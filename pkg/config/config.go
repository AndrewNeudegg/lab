package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	AgentName       string                         `json:"agent_name"`
	DefaultProvider string                         `json:"default_provider"`
	Providers       map[string]ProviderConfig      `json:"providers"`
	Repo            RepoConfig                     `json:"repo"`
	Policy          PolicyConfig                   `json:"policy"`
	Limits          LimitsConfig                   `json:"limits"`
	DataDir         string                         `json:"data_dir"`
	HTTP            HTTPConfig                     `json:"http"`
	Matrix          MatrixConfig                   `json:"matrix"`
	ExternalAgents  map[string]ExternalAgentConfig `json:"external_agents"`
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

type MatrixConfig struct {
	Homeserver    string `json:"homeserver"`
	User          string `json:"user"`
	Password      string `json:"password,omitempty"`
	AccessToken   string `json:"access_token,omitempty"`
	RoomID        string `json:"room_id,omitempty"`
	RoomAlias     string `json:"room_alias,omitempty"`
	RoomName      string `json:"room_name,omitempty"`
	RequirePrefix bool   `json:"require_prefix"`
	Prefix        string `json:"prefix,omitempty"`
	SyncTimeoutMS int    `json:"sync_timeout_ms"`
}

type ExternalAgentConfig struct {
	Enabled        bool     `json:"enabled"`
	Command        string   `json:"command"`
	Args           []string `json:"args,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds"`
	Description    string   `json:"description,omitempty"`
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
		HTTP:    HTTPConfig{Addr: "127.0.0.1:18080"},
		Matrix: MatrixConfig{
			Homeserver:    getenvAny("MATRIX_HOMESERVER", "ELEMENT_HOMESERVER"),
			User:          getenvAny("MATRIX_USER", "ELEMENT_BOT_USERNAME"),
			Password:      getenvAny("MATRIX_PASSWORD", "ELEMENT_BOT_PASSWORD"),
			AccessToken:   getenvAny("MATRIX_ACCESS_TOKEN", "ELEMENT_BOT_ACCESS_TOKEN"),
			RoomID:        getenvAny("MATRIX_ROOM_ID", "ELEMENT_ROOM_ID"),
			RoomAlias:     getenvAny("MATRIX_ROOM_ALIAS", "ELEMENT_ROOM_ALIAS"),
			RoomName:      getenvAny("MATRIX_ROOM_NAME", "ELEMENT_ROOM_NAME"),
			RequirePrefix: true,
			Prefix:        getenvAny("MATRIX_PREFIX", "ELEMENT_BOT_PREFIX"),
			SyncTimeoutMS: 30000,
		},
		ExternalAgents: map[string]ExternalAgentConfig{
			"codex": {
				Enabled:        true,
				Command:        getenvAnyDefault("codex", "CODEX_CLI", "CODEX_CMD"),
				Args:           []string{"exec", "--skip-git-repo-check"},
				TimeoutSeconds: 900,
				Description:    "OpenAI Codex CLI worker for coding tasks.",
			},
			"claude": {
				Enabled:        true,
				Command:        getenvAnyDefault("claude", "CLAUDE_CLI", "CLAUDE_CMD"),
				Args:           []string{},
				TimeoutSeconds: 900,
				Description:    "Claude CLI worker for analysis or coding tasks.",
			},
			"gemini": {
				Enabled:        true,
				Command:        getenvAnyDefault("gemini", "GEMINI_CLI", "GEMINI_CMD"),
				Args:           []string{},
				TimeoutSeconds: 900,
				Description:    "Gemini CLI worker for analysis or coding tasks.",
			},
		},
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
	if c.Matrix.Homeserver == "" {
		c.Matrix.Homeserver = "http://lab:8008"
	}
	if c.Matrix.User == "" {
		c.Matrix.User = d.Matrix.User
	}
	if c.Matrix.Password == "" {
		c.Matrix.Password = d.Matrix.Password
	}
	if c.Matrix.AccessToken == "" {
		c.Matrix.AccessToken = d.Matrix.AccessToken
	}
	if c.Matrix.RoomID == "" {
		c.Matrix.RoomID = d.Matrix.RoomID
	}
	if c.Matrix.RoomAlias == "" {
		c.Matrix.RoomAlias = d.Matrix.RoomAlias
	}
	if c.Matrix.RoomName == "" {
		c.Matrix.RoomName = "first"
	}
	if c.Matrix.Prefix == "" {
		c.Matrix.Prefix = "!agent"
	}
	if c.Matrix.SyncTimeoutMS == 0 {
		c.Matrix.SyncTimeoutMS = d.Matrix.SyncTimeoutMS
	}
	if c.ExternalAgents == nil {
		c.ExternalAgents = d.ExternalAgents
	} else {
		for name, agent := range d.ExternalAgents {
			if _, ok := c.ExternalAgents[name]; !ok {
				c.ExternalAgents[name] = agent
			}
		}
		for name, agent := range c.ExternalAgents {
			if agent.Command == "" {
				agent.Command = d.ExternalAgents[name].Command
			}
			if agent.Args == nil {
				agent.Args = d.ExternalAgents[name].Args
			}
			if agent.TimeoutSeconds == 0 {
				agent.TimeoutSeconds = d.ExternalAgents[name].TimeoutSeconds
			}
			if agent.Description == "" {
				agent.Description = d.ExternalAgents[name].Description
			}
			c.ExternalAgents[name] = agent
		}
	}
	return c
}

func getenvAny(keys ...string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return ""
}

func getenvAnyDefault(defaultValue string, keys ...string) string {
	if value := getenvAny(keys...); value != "" {
		return value
	}
	return defaultValue
}
