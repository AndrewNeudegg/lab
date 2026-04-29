package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const DefaultExternalAgentTimeoutSeconds = 5 * 60 * 60

type Config struct {
	AgentName       string                         `json:"agent_name"`
	DefaultProvider string                         `json:"default_provider"`
	Providers       map[string]ProviderConfig      `json:"providers"`
	Repo            RepoConfig                     `json:"repo"`
	Policy          PolicyConfig                   `json:"policy"`
	Limits          LimitsConfig                   `json:"limits"`
	DataDir         string                         `json:"data_dir"`
	HTTP            HTTPConfig                     `json:"http"`
	ControlPlane    ControlPlaneConfig             `json:"control_plane"`
	RemoteAgent     RemoteAgentConfig              `json:"remote_agent"`
	Healthd         HealthdConfig                  `json:"healthd"`
	Supervisord     SupervisordConfig              `json:"supervisord"`
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

type ControlPlaneConfig struct {
	AgentToken        string `json:"agent_token,omitempty"`
	AgentTokenEnv     string `json:"agent_token_env,omitempty"`
	AgentStaleSeconds int    `json:"agent_stale_seconds"`
}

type RemoteAgentConfig struct {
	ID                       string                     `json:"id,omitempty"`
	Name                     string                     `json:"name,omitempty"`
	Machine                  string                     `json:"machine,omitempty"`
	APIBase                  string                     `json:"api_base,omitempty"`
	Token                    string                     `json:"token,omitempty"`
	TokenEnv                 string                     `json:"token_env,omitempty"`
	Backend                  string                     `json:"backend,omitempty"`
	TerminalAddr             string                     `json:"terminal_addr,omitempty"`
	TerminalPublicURL        string                     `json:"terminal_public_url,omitempty"`
	HeartbeatIntervalSeconds int                        `json:"heartbeat_interval_seconds"`
	PollIntervalSeconds      int                        `json:"poll_interval_seconds"`
	Workdirs                 []RemoteAgentWorkdirConfig `json:"workdirs,omitempty"`
}

type RemoteAgentWorkdirConfig struct {
	ID    string `json:"id,omitempty"`
	Path  string `json:"path"`
	Label string `json:"label,omitempty"`
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
	LogDir                   string                `json:"log_dir,omitempty"`
	WorkingDir               string                `json:"working_dir,omitempty"`
	RestartCommand           string                `json:"restart_command,omitempty"`
	RestartArgs              []string              `json:"restart_args,omitempty"`
	Apps                     []SupervisorAppConfig `json:"apps,omitempty"`
}

type SupervisorAppConfig struct {
	Name                   string            `json:"name"`
	Type                   string            `json:"type,omitempty"`
	Command                string            `json:"command"`
	Args                   []string          `json:"args,omitempty"`
	WorkingDir             string            `json:"working_dir,omitempty"`
	Env                    map[string]string `json:"env,omitempty"`
	PreStartCommand        string            `json:"pre_start_command,omitempty"`
	PreStartArgs           []string          `json:"pre_start_args,omitempty"`
	PreStartWorkingDir     string            `json:"pre_start_working_dir,omitempty"`
	PreStartTimeoutSeconds int               `json:"pre_start_timeout_seconds,omitempty"`
	StartOrder             int               `json:"start_order"`
	AutoStart              bool              `json:"auto_start"`
	Restart                string            `json:"restart,omitempty"`
	HealthURL              string            `json:"health_url,omitempty"`
	ShutdownTimeoutSec     int               `json:"shutdown_timeout_seconds,omitempty"`
}

type ExternalAgentConfig struct {
	Enabled        bool              `json:"enabled"`
	Command        string            `json:"command"`
	Args           []string          `json:"args,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds"`
	Description    string            `json:"description,omitempty"`
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
		ControlPlane: ControlPlaneConfig{
			AgentTokenEnv:     "HOMELABD_AGENT_TOKEN",
			AgentStaleSeconds: 30,
		},
		RemoteAgent: RemoteAgentConfig{
			APIBase:                  "http://127.0.0.1:18080",
			TokenEnv:                 "HOMELABD_AGENT_TOKEN",
			Backend:                  "codex",
			HeartbeatIntervalSeconds: 10,
			PollIntervalSeconds:      5,
		},
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
			LogDir:                   filepath.Join("data", "supervisord", "logs"),
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
					Args:               []string{"run", "./cmd/homelabd", "-mode", "http"},
					WorkingDir:         ".",
					StartOrder:         20,
					AutoStart:          false,
					Restart:            "always",
					HealthURL:          "http://127.0.0.1:18080/healthz",
					ShutdownTimeoutSec: 15,
				},
				{
					Name:                   "dashboard",
					Type:                   "web",
					Command:                "bun",
					Args:                   []string{"run", "dev", "--", "--host", "0.0.0.0", "--port", "5173", "--strictPort"},
					WorkingDir:             "web/dashboard",
					PreStartCommand:        "bun",
					PreStartArgs:           []string{"install", "--frozen-lockfile"},
					PreStartWorkingDir:     "web",
					PreStartTimeoutSeconds: 300,
					StartOrder:             30,
					AutoStart:              false,
					Restart:                "on_failure",
					HealthURL:              "http://127.0.0.1:5173/chat",
					ShutdownTimeoutSec:     10,
				},
				{
					Name:               "homelab-agent",
					Type:               "agent",
					Command:            "go",
					Args:               []string{"run", "./cmd/homelab-agent"},
					WorkingDir:         ".",
					StartOrder:         40,
					AutoStart:          false,
					Restart:            "always",
					ShutdownTimeoutSec: 15,
				},
			},
		},
		ExternalAgents: map[string]ExternalAgentConfig{
			"codex": {
				Enabled:        true,
				Command:        getenvAnyDefault("codex", "CODEX_CLI", "CODEX_CMD"),
				Args:           []string{"--dangerously-bypass-approvals-and-sandbox", "exec", "--skip-git-repo-check"},
				Env:            map[string]string{"CODEX_UNSAFE_ALLOW_NO_SANDBOX": "1"},
				TimeoutSeconds: DefaultExternalAgentTimeoutSeconds,
				Description:    "OpenAI Codex CLI worker for trusted isolated task worktrees.",
			},
			"claude": {
				Enabled:        true,
				Command:        getenvAnyDefault("claude", "CLAUDE_CLI", "CLAUDE_CMD"),
				Args:           []string{},
				TimeoutSeconds: DefaultExternalAgentTimeoutSeconds,
				Description:    "Claude CLI worker for analysis or coding tasks.",
			},
			"gemini": {
				Enabled:        true,
				Command:        getenvAnyDefault("gemini", "GEMINI_CLI", "GEMINI_CMD"),
				Args:           []string{},
				TimeoutSeconds: DefaultExternalAgentTimeoutSeconds,
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
	if c.ControlPlane.AgentTokenEnv == "" {
		c.ControlPlane.AgentTokenEnv = d.ControlPlane.AgentTokenEnv
	}
	if c.ControlPlane.AgentToken == "" && c.ControlPlane.AgentTokenEnv != "" {
		c.ControlPlane.AgentToken = os.Getenv(c.ControlPlane.AgentTokenEnv)
	}
	if c.ControlPlane.AgentStaleSeconds == 0 {
		c.ControlPlane.AgentStaleSeconds = d.ControlPlane.AgentStaleSeconds
	}
	if c.RemoteAgent.APIBase == "" {
		c.RemoteAgent.APIBase = d.RemoteAgent.APIBase
	}
	if c.RemoteAgent.TokenEnv == "" {
		c.RemoteAgent.TokenEnv = d.RemoteAgent.TokenEnv
	}
	if c.RemoteAgent.Token == "" && c.RemoteAgent.TokenEnv != "" {
		c.RemoteAgent.Token = os.Getenv(c.RemoteAgent.TokenEnv)
	}
	if c.RemoteAgent.Backend == "" {
		c.RemoteAgent.Backend = d.RemoteAgent.Backend
	}
	if c.RemoteAgent.TerminalAddr == "" {
		c.RemoteAgent.TerminalAddr = d.RemoteAgent.TerminalAddr
	}
	if c.RemoteAgent.HeartbeatIntervalSeconds == 0 {
		c.RemoteAgent.HeartbeatIntervalSeconds = d.RemoteAgent.HeartbeatIntervalSeconds
	}
	if c.RemoteAgent.PollIntervalSeconds == 0 {
		c.RemoteAgent.PollIntervalSeconds = d.RemoteAgent.PollIntervalSeconds
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
	if c.Supervisord.LogDir == "" {
		c.Supervisord.LogDir = filepath.Join(c.DataDir, "supervisord", "logs")
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
	if c.ExternalAgents == nil {
		c.ExternalAgents = d.ExternalAgents
	} else {
		for name, agent := range d.ExternalAgents {
			if _, ok := c.ExternalAgents[name]; !ok {
				c.ExternalAgents[name] = agent
			}
		}
		for name, agent := range c.ExternalAgents {
			defaultAgent := d.ExternalAgents[name]
			if agent.Command == "" {
				agent.Command = defaultAgent.Command
			}
			if agent.Args == nil {
				agent.Args = defaultAgent.Args
			}
			agent.Env = mergeDefaultEnv(agent.Env, defaultAgent.Env)
			if agent.TimeoutSeconds == 0 {
				agent.TimeoutSeconds = defaultAgent.TimeoutSeconds
			}
			if agent.Description == "" {
				agent.Description = defaultAgent.Description
			}
			c.ExternalAgents[name] = agent
		}
	}
	return c
}

func mergeDefaultEnv(env, defaults map[string]string) map[string]string {
	if len(defaults) == 0 {
		return env
	}
	if env == nil {
		env = map[string]string{}
	}
	for key, value := range defaults {
		if _, ok := env[key]; !ok {
			env[key] = value
		}
	}
	return env
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
