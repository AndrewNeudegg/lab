package config

import (
	"strings"
	"testing"
)

func TestDefaultIncludesRemoteAgentAndControlPlaneConfig(t *testing.T) {
	cfg := Default()

	if cfg.ControlPlane.AgentTokenEnv != "HOMELABD_AGENT_TOKEN" {
		t.Fatalf("control plane token env = %q", cfg.ControlPlane.AgentTokenEnv)
	}
	if cfg.ControlPlane.AgentStaleSeconds != 30 {
		t.Fatalf("agent stale seconds = %d", cfg.ControlPlane.AgentStaleSeconds)
	}
	if cfg.RemoteAgent.APIBase == "" || cfg.RemoteAgent.Backend != "codex" {
		t.Fatalf("remote agent config = %#v", cfg.RemoteAgent)
	}
}

func TestDefaultExternalAgentsUseFiveHourTimeout(t *testing.T) {
	const want = 5 * 60 * 60
	if DefaultExternalAgentTimeoutSeconds != want {
		t.Fatalf("default external agent timeout = %d, want %d", DefaultExternalAgentTimeoutSeconds, want)
	}
	cfg := Default()
	for name, agent := range cfg.ExternalAgents {
		if agent.TimeoutSeconds != want {
			t.Fatalf("external agent %q timeout = %d, want %d", name, agent.TimeoutSeconds, want)
		}
	}
}

func TestDefaultCodexAgentBypassesCodexSandboxForTaskWorktrees(t *testing.T) {
	cfg := Default()
	codex := cfg.ExternalAgents["codex"]
	if codex.Command != "codex" {
		t.Fatalf("codex command = %q, want codex", codex.Command)
	}
	gotArgs := strings.Join(codex.Args, " ")
	if !strings.Contains(gotArgs, "--dangerously-bypass-approvals-and-sandbox") || !strings.Contains(gotArgs, "exec") {
		t.Fatalf("codex args = %q, want sandbox bypass before exec", gotArgs)
	}
	if codex.Env["CODEX_UNSAFE_ALLOW_NO_SANDBOX"] != "1" {
		t.Fatalf("codex env = %#v, want CODEX_UNSAFE_ALLOW_NO_SANDBOX=1", codex.Env)
	}
}

func TestWithDefaultsPreservesCustomCodexArgs(t *testing.T) {
	cfg := Config{
		ExternalAgents: map[string]ExternalAgentConfig{
			"codex": {
				Enabled: true,
				Command: "codex",
				Args:    []string{"exec", "--model", "custom"},
				Env:     map[string]string{"CODEX_UNSAFE_ALLOW_NO_SANDBOX": "0"},
			},
		},
	}
	got := cfg.WithDefaults().ExternalAgents["codex"]
	if strings.Join(got.Args, " ") != "exec --model custom" {
		t.Fatalf("codex args = %#v, want custom args preserved", got.Args)
	}
	if got.Env["CODEX_UNSAFE_ALLOW_NO_SANDBOX"] != "0" {
		t.Fatalf("codex env = %#v, want custom env preserved", got.Env)
	}
}

func TestDefaultSupervisorIncludesDisabledRemoteAgentTemplate(t *testing.T) {
	cfg := Default()
	var agentApp *SupervisorAppConfig
	for i := range cfg.Supervisord.Apps {
		if cfg.Supervisord.Apps[i].Name == "homelab-agent" {
			agentApp = &cfg.Supervisord.Apps[i]
			break
		}
	}
	if agentApp == nil {
		t.Fatal("default supervisor apps missing homelab-agent template")
	}
	if agentApp.Type != "agent" || agentApp.AutoStart {
		t.Fatalf("agent app = %#v, want type agent and autostart false", *agentApp)
	}
	if agentApp.Restart != "always" {
		t.Fatalf("agent app restart = %q, want always", agentApp.Restart)
	}
}

func TestDefaultSupervisorRunsHomelabdHTTPMode(t *testing.T) {
	cfg := Default()
	var homelabdApp *SupervisorAppConfig
	for i := range cfg.Supervisord.Apps {
		if cfg.Supervisord.Apps[i].Name == "homelabd" {
			homelabdApp = &cfg.Supervisord.Apps[i]
			break
		}
	}
	if homelabdApp == nil {
		t.Fatal("default supervisor apps missing homelabd")
	}
	got := strings.Join(homelabdApp.Args, " ")
	if got != "run ./cmd/homelabd -mode http" {
		t.Fatalf("homelabd args = %q, want http mode", got)
	}
}

func TestDefaultSupervisorPreparesDashboardDependencies(t *testing.T) {
	cfg := Default()
	var dashboardApp *SupervisorAppConfig
	for i := range cfg.Supervisord.Apps {
		if cfg.Supervisord.Apps[i].Name == "dashboard" {
			dashboardApp = &cfg.Supervisord.Apps[i]
			break
		}
	}
	if dashboardApp == nil {
		t.Fatal("default supervisor apps missing dashboard")
	}
	if dashboardApp.PreStartCommand != "bun" {
		t.Fatalf("dashboard pre-start command = %q, want bun", dashboardApp.PreStartCommand)
	}
	if got := strings.Join(dashboardApp.PreStartArgs, " "); got != "install --frozen-lockfile" {
		t.Fatalf("dashboard pre-start args = %q, want frozen install", got)
	}
	if dashboardApp.PreStartWorkingDir != "web" {
		t.Fatalf("dashboard pre-start working dir = %q, want web", dashboardApp.PreStartWorkingDir)
	}
	if dashboardApp.PreStartTimeoutSeconds < 300 {
		t.Fatalf("dashboard pre-start timeout = %d, want at least 300", dashboardApp.PreStartTimeoutSeconds)
	}
}

func TestDefaultExternalAgentTimeoutsAreFiveHours(t *testing.T) {
	const want = 5 * 60 * 60
	if DefaultExternalAgentTimeoutSeconds != want {
		t.Fatalf("default external agent timeout = %d, want %d", DefaultExternalAgentTimeoutSeconds, want)
	}
	cfg := Default()
	for name, agent := range cfg.ExternalAgents {
		if agent.TimeoutSeconds != want {
			t.Fatalf("external agent %q timeout = %d, want %d", name, agent.TimeoutSeconds, want)
		}
	}
}

func TestWithDefaultsPreservesRemoteAgentWorkdirsAndFillsIntervals(t *testing.T) {
	cfg := Config{
		RemoteAgent: RemoteAgentConfig{
			ID:       "desk",
			Workdirs: []RemoteAgentWorkdirConfig{{ID: "repo", Path: "/srv/repo"}},
		},
	}
	got := cfg.WithDefaults()

	if got.RemoteAgent.ID != "desk" || len(got.RemoteAgent.Workdirs) != 1 {
		t.Fatalf("remote agent identity/workdirs = %#v", got.RemoteAgent)
	}
	if got.RemoteAgent.APIBase == "" || got.RemoteAgent.Backend != "codex" {
		t.Fatalf("remote agent defaults = %#v", got.RemoteAgent)
	}
	if got.RemoteAgent.HeartbeatIntervalSeconds == 0 || got.RemoteAgent.PollIntervalSeconds == 0 {
		t.Fatalf("remote agent intervals not defaulted: %#v", got.RemoteAgent)
	}
}
