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
	Healthd         HealthdConfig                  `json:"healthd"`
	Supervisord     SupervisordConfig              `json:"supervisord"`
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
	TaskWatchdogSeconds int   `json:"task_watchdog_seconds"`
	TaskStaleSeconds    int   `json:"task_stale_seconds"`
}

type HTTPConfig struct {
	Addr string `json:"addr"`
}

type HealthdConfig struct {
	Addr                            string                     `json:"addr"`
	Enabled                         *bool                      `json:"enabled"`
	SampleIntervalSeconds           int                        `json:"sample_interval_seconds"`
	RetentionSeconds                int                        `json:"retention_seconds"`
	RequestTimeoutSeconds           int                        `json:"request_timeout_seconds"`
	ProcessHeartbeatIntervalSeconds int                        `json:"process_heartbeat_interval_seconds"`
	ProcessTimeoutSeconds           int                        `json:"process_timeout_seconds"`
	Checks                          []HealthCheckConfig        `json:"checks,omitempty"`
	SLOs                            []HealthSLOConfig          `json:"slos,omitempty"`
	Notifications                   []HealthNotificationConfig `json:"notifications,omitempty"`
}

type HealthCheckConfig struct {
	Name           string `json:"name"`
	Type           string `json:"type"`
	URL            string `json:"url,omitempty"`
	Method         string `json:"method,omitempty"`
	ExpectStatus   int    `json:"expect_status,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

type HealthSLOConfig struct {
	Name            string  `json:"name"`
	TargetPercent   float64 `json:"target_percent"`
	WindowSeconds   int     `json:"window_seconds"`
	WarningBurnRate float64 `json:"warning_burn_rate"`
	PageBurnRate    float64 `json:"page_burn_rate"`
}

type HealthNotificationConfig struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	URL         string `json:"url,omitempty"`
	MinSeverity string `json:"min_severity,omitempty"`
}

type SupervisordConfig struct {
	Addr                     string                `json:"addr"`
	HealthdURL               string                `json:"healthd_url"`
	HeartbeatIntervalSeconds int                   `json:"heartbeat_interval_seconds"`
	ShutdownTimeoutSeconds   int                   `json:"shutdown_timeout_seconds"`
	StatePath                string                `json:"state_path,omitempty"`
	WorkingDir               string                `json:"working_dir,omitempty"`
	RestartCommand           string                `json:"restart_command,omitempty"`
	RestartArgs              []string              `json:"restart_args,omitempty"`
	Apps                     []SupervisorAppConfig `json:"apps,omitempty"`
}

type SupervisorAppConfig struct {
	Name               string            `json:"name"`
	Type               string            `json:"type,omitempty"`
	Command            string            `json:"command"`
	Args               []string          `json:"args,omitempty"`
	WorkingDir         string            `json:"working_dir,omitempty"`
	Env                map[string]string `json:"env,omitempty"`
	StartOrder         int               `json:"start_order"`
	AutoStart          bool              `json:"auto_start"`
	Restart            string            `json:"restart,omitempty"`
	HealthURL          string            `json:"health_url,omitempty"`
	ShutdownTimeoutSec int               `json:"shutdown_timeout_seconds,omitempty"`
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
				"git.revert",
				"git.merge",
				"repo.apply_patch_to_main",
				"git.merge_approved",
			},
		},
		Limits: LimitsConfig{
			MaxConcurrentTasks:  4,
			MaxToolCallsPerTurn: 12,
			MaxShellSeconds:     60,
			MaxFileBytes:        1 << 20,
			TaskWatchdogSeconds: 30,
			TaskStaleSeconds:    300,
		},
		DataDir: "data",
		HTTP:    HTTPConfig{Addr: "127.0.0.1:18080"},
		Healthd: HealthdConfig{
			Addr:                            "127.0.0.1:18081",
			Enabled:                         boolPtr(true),
			SampleIntervalSeconds:           5,
			RetentionSeconds:                300,
			RequestTimeoutSeconds:           2,
			ProcessHeartbeatIntervalSeconds: 5,
			ProcessTimeoutSeconds:           15,
			SLOs: []HealthSLOConfig{
				{
					Name:            "availability",
					TargetPercent:   99.9,
					WindowSeconds:   300,
					WarningBurnRate: 2,
					PageBurnRate:    10,
				},
			},
		},
		Supervisord: SupervisordConfig{
			Addr:                     "127.0.0.1:18082",
			HealthdURL:               "http://127.0.0.1:18081",
			HeartbeatIntervalSeconds: 5,
			ShutdownTimeoutSeconds:   10,
			StatePath:                filepath.Join("data", "supervisord", "state.json"),
			WorkingDir:               ".",
			RestartCommand:           "go",
			RestartArgs:              []string{"run", "./cmd/supervisord"},
			Apps: []SupervisorAppConfig{
				{
					Name:               "healthd",
					Type:               "daemon",
					Command:            "go",
					Args:               []string{"run", "./cmd/healthd"},
					WorkingDir:         ".",
					StartOrder:         10,
					AutoStart:          false,
					Restart:            "always",
					HealthURL:          "http://127.0.0.1:18081/healthd",
					ShutdownTimeoutSec: 10,
				},
				{
					Name:               "homelabd",
					Type:               "daemon",
					Command:            "go",
					Args:               []string{"run", "./cmd/homelabd", "-mode", "matrix"},
					WorkingDir:         ".",
					StartOrder:         20,
					AutoStart:          false,
					Restart:            "always",
					HealthURL:          "http://127.0.0.1:18080/healthz",
					ShutdownTimeoutSec: 15,
				},
				{
					Name:               "dashboard",
					Type:               "web",
					Command:            "bun",
					Args:               []string{"run", "dev", "--", "--host", "0.0.0.0"},
					WorkingDir:         "web/dashboard",
					StartOrder:         30,
					AutoStart:          false,
					Restart:            "on_failure",
					HealthURL:          "http://127.0.0.1:5173/chat",
					ShutdownTimeoutSec: 10,
				},
			},
		},
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
	if c.Limits.TaskWatchdogSeconds == 0 {
		c.Limits.TaskWatchdogSeconds = d.Limits.TaskWatchdogSeconds
	}
	if c.Limits.TaskStaleSeconds == 0 {
		c.Limits.TaskStaleSeconds = d.Limits.TaskStaleSeconds
	}
	if c.DataDir == "" {
		c.DataDir = d.DataDir
	}
	if c.HTTP.Addr == "" {
		c.HTTP.Addr = d.HTTP.Addr
	}
	if c.Healthd.Enabled == nil {
		c.Healthd.Enabled = d.Healthd.Enabled
	}
	if c.Healthd.Addr == "" {
		c.Healthd.Addr = d.Healthd.Addr
	}
	if c.Healthd.SampleIntervalSeconds == 0 {
		c.Healthd.SampleIntervalSeconds = d.Healthd.SampleIntervalSeconds
	}
	if c.Healthd.RetentionSeconds == 0 {
		c.Healthd.RetentionSeconds = d.Healthd.RetentionSeconds
	}
	if c.Healthd.RequestTimeoutSeconds == 0 {
		c.Healthd.RequestTimeoutSeconds = d.Healthd.RequestTimeoutSeconds
	}
	if c.Healthd.ProcessHeartbeatIntervalSeconds == 0 {
		c.Healthd.ProcessHeartbeatIntervalSeconds = d.Healthd.ProcessHeartbeatIntervalSeconds
	}
	if c.Healthd.ProcessTimeoutSeconds == 0 {
		c.Healthd.ProcessTimeoutSeconds = d.Healthd.ProcessTimeoutSeconds
	}
	if c.Healthd.SLOs == nil {
		c.Healthd.SLOs = d.Healthd.SLOs
	} else {
		for i, slo := range c.Healthd.SLOs {
			if slo.TargetPercent == 0 {
				slo.TargetPercent = 99.9
			}
			if slo.WindowSeconds == 0 {
				slo.WindowSeconds = c.Healthd.RetentionSeconds
			}
			if slo.WarningBurnRate == 0 {
				slo.WarningBurnRate = 2
			}
			if slo.PageBurnRate == 0 {
				slo.PageBurnRate = 10
			}
			c.Healthd.SLOs[i] = slo
		}
	}
	if c.Supervisord.Addr == "" {
		c.Supervisord.Addr = d.Supervisord.Addr
	}
	if c.Supervisord.HealthdURL == "" {
		c.Supervisord.HealthdURL = d.Supervisord.HealthdURL
	}
	if c.Supervisord.HeartbeatIntervalSeconds == 0 {
		c.Supervisord.HeartbeatIntervalSeconds = d.Supervisord.HeartbeatIntervalSeconds
	}
	if c.Supervisord.ShutdownTimeoutSeconds == 0 {
		c.Supervisord.ShutdownTimeoutSeconds = d.Supervisord.ShutdownTimeoutSeconds
	}
	if c.Supervisord.StatePath == "" {
		c.Supervisord.StatePath = filepath.Join(c.DataDir, "supervisord", "state.json")
	}
	if c.Supervisord.WorkingDir == "" {
		c.Supervisord.WorkingDir = d.Supervisord.WorkingDir
	}
	if c.Supervisord.RestartCommand == "" {
		c.Supervisord.RestartCommand = d.Supervisord.RestartCommand
	}
	if c.Supervisord.RestartArgs == nil {
		c.Supervisord.RestartArgs = d.Supervisord.RestartArgs
	}
	if c.Supervisord.Apps == nil {
		c.Supervisord.Apps = d.Supervisord.Apps
	}
	for i, app := range c.Supervisord.Apps {
		if app.Type == "" {
			app.Type = "process"
		}
		if app.Restart == "" {
			app.Restart = "on_failure"
		}
		if app.ShutdownTimeoutSec == 0 {
			app.ShutdownTimeoutSec = c.Supervisord.ShutdownTimeoutSeconds
		}
		c.Supervisord.Apps[i] = app
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

func boolPtr(value bool) *bool {
	return &value
}
