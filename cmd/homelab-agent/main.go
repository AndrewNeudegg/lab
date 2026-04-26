package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/andrewneudegg/lab/pkg/config"
	controlserver "github.com/andrewneudegg/lab/pkg/control"
	agentrunner "github.com/andrewneudegg/lab/pkg/externalagent"
	"github.com/andrewneudegg/lab/pkg/remoteagent"
)

const version = "dev"

type workdirFlags []remoteagent.Workdir

func (w *workdirFlags) String() string {
	parts := make([]string, 0, len(*w))
	for _, workdir := range *w {
		parts = append(parts, workdir.Path)
	}
	return strings.Join(parts, ",")
}

func (w *workdirFlags) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	id := ""
	label := ""
	path := value
	if left, right, ok := strings.Cut(value, "="); ok {
		id = strings.TrimSpace(left)
		path = strings.TrimSpace(right)
		label = id
	}
	if path == "" {
		return fmt.Errorf("workdir path is required")
	}
	*w = append(*w, remoteagent.Workdir{ID: id, Path: path, Label: label})
	return nil
}

func main() {
	configPath := flag.String("config", "config.json", "configuration file")
	apiBase := flag.String("api", "", "homelabd API base URL")
	agentID := flag.String("id", "", "remote agent id")
	name := flag.String("name", "", "remote agent display name")
	machine := flag.String("machine", "", "machine name")
	backend := flag.String("backend", "", "external worker backend to run for assignments")
	terminalAddr := flag.String("terminal-addr", "", "optional HTTP address for remote terminal sessions")
	terminalURL := flag.String("terminal-url", "", "browser-reachable terminal API base URL advertised to homelabd")
	var workdirs workdirFlags
	flag.Var(&workdirs, "workdir", "allowed working directory, optionally id=/path")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if home, err := os.UserHomeDir(); err == nil {
		if err := config.LoadDotEnv(filepath.Join(home, ".env")); err != nil {
			fatal(err)
		}
	}
	if err := config.LoadDotEnv(".env"); err != nil {
		fatal(err)
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		fatal(err)
	}
	agentCfg := cfg.RemoteAgent
	if *apiBase != "" {
		agentCfg.APIBase = *apiBase
	}
	if *agentID != "" {
		agentCfg.ID = *agentID
	}
	if *name != "" {
		agentCfg.Name = *name
	}
	if *machine != "" {
		agentCfg.Machine = *machine
	}
	if *backend != "" {
		agentCfg.Backend = *backend
	}
	if *terminalAddr != "" {
		agentCfg.TerminalAddr = *terminalAddr
	}
	if *terminalURL != "" {
		agentCfg.TerminalPublicURL = *terminalURL
	}
	if len(workdirs) > 0 {
		agentCfg.Workdirs = make([]config.RemoteAgentWorkdirConfig, 0, len(workdirs))
		for _, workdir := range workdirs {
			agentCfg.Workdirs = append(agentCfg.Workdirs, config.RemoteAgentWorkdirConfig{ID: workdir.ID, Path: workdir.Path, Label: workdir.Label})
		}
	}
	if err := run(ctx, cfg, agentCfg); err != nil {
		fatal(err)
	}
}

func run(ctx context.Context, cfg config.Config, agentCfg config.RemoteAgentConfig) error {
	hostname, _ := os.Hostname()
	if agentCfg.Machine == "" {
		agentCfg.Machine = hostname
	}
	if agentCfg.ID == "" {
		agentCfg.ID = firstNonEmpty(agentCfg.Machine, hostname, "homelab-agent")
	}
	if agentCfg.Name == "" {
		agentCfg.Name = agentCfg.ID
	}
	if agentCfg.Backend == "" {
		agentCfg.Backend = "codex"
	}
	if len(agentCfg.Workdirs) == 0 {
		agentCfg.Workdirs = []config.RemoteAgentWorkdirConfig{{ID: "repo", Path: cfg.Repo.Root, Label: "Repo"}}
	}
	client := controlClient{
		base:  strings.TrimRight(agentCfg.APIBase, "/"),
		token: agentCfg.Token,
		http:  &http.Client{Timeout: 30 * time.Second},
	}
	if client.base == "" {
		return fmt.Errorf("remote_agent.api_base is required")
	}
	if client.token == "" {
		return fmt.Errorf("remote agent token is required; set remote_agent.token or %s", firstNonEmpty(agentCfg.TokenEnv, "HOMELABD_AGENT_TOKEN"))
	}
	workdirs := remoteWorkdirs(agentCfg.Workdirs)
	if len(workdirs) == 0 {
		return fmt.Errorf("at least one remote_agent.workdirs path is required")
	}
	terminalBaseURL := strings.TrimRight(strings.TrimSpace(agentCfg.TerminalPublicURL), "/")
	if strings.TrimSpace(agentCfg.TerminalAddr) != "" {
		server := controlserver.Server{Addr: strings.TrimSpace(agentCfg.TerminalAddr), TerminalOnly: true}
		go func() {
			slog.Info("remote terminal listening", "addr", server.Addr, "public_url", terminalBaseURL)
			if err := server.Listen(ctx); err != nil && ctx.Err() == nil {
				slog.Error("remote terminal server failed", "error", err)
			}
		}()
	}
	startedAt := time.Now().UTC()
	heartbeatEvery := time.Duration(agentCfg.HeartbeatIntervalSeconds) * time.Second
	if heartbeatEvery <= 0 {
		heartbeatEvery = 10 * time.Second
	}
	pollEvery := time.Duration(agentCfg.PollIntervalSeconds) * time.Second
	if pollEvery <= 0 {
		pollEvery = 5 * time.Second
	}
	runner := agentrunner.NewRunner(cfg.ExternalAgents)
	currentTaskID := ""
	capabilities, metadata := remoteAgentMetadata(agentCfg.Backend, terminalBaseURL)
	sendHeartbeat := func() {
		err := client.heartbeat(ctx, remoteagent.Heartbeat{
			ID:            agentCfg.ID,
			Name:          agentCfg.Name,
			Machine:       agentCfg.Machine,
			Version:       version,
			StartedAt:     startedAt,
			Capabilities:  capabilities,
			Workdirs:      workdirs,
			CurrentTaskID: currentTaskID,
			Metadata:      metadata,
		})
		if err != nil {
			slog.Warn("remote heartbeat failed", "error", err)
		}
	}
	sendHeartbeat()
	heartbeatTicker := time.NewTicker(heartbeatEvery)
	defer heartbeatTicker.Stop()
	pollTicker := time.NewTicker(pollEvery)
	defer pollTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-heartbeatTicker.C:
			sendHeartbeat()
		case <-pollTicker.C:
			if currentTaskID != "" {
				continue
			}
			assignment, err := client.claim(ctx, agentCfg.ID, agentCfg.Backend)
			if err != nil {
				slog.Warn("remote claim failed", "error", err)
				continue
			}
			if assignment == nil {
				continue
			}
			currentTaskID = assignment.TaskID
			sendHeartbeat()
			slog.Info("remote assignment claimed", "task_id", assignment.TaskID, "workdir", assignment.Workdir, "backend", assignment.Backend)
			if err := executeAssignment(ctx, client, runner, agentCfg.ID, agentCfg.Backend, assignment); err != nil {
				slog.Warn("remote completion failed", "task_id", assignment.TaskID, "error", err)
			}
			currentTaskID = ""
			sendHeartbeat()
		}
	}
}

type agentControl interface {
	complete(ctx context.Context, agentID, taskID, status, result, errorText string) error
}

type assignmentRunner interface {
	Run(ctx context.Context, req agentrunner.RunRequest) (agentrunner.RunResult, error)
}

func executeAssignment(ctx context.Context, client agentControl, runner assignmentRunner, agentID, fallbackBackend string, assignment *remoteagent.Assignment) error {
	if assignment == nil {
		return nil
	}
	result, runErr := runner.Run(ctx, agentrunner.RunRequest{
		Backend:     firstNonEmpty(assignment.Backend, fallbackBackend),
		TaskID:      assignment.TaskID,
		Workspace:   assignment.Workdir,
		Instruction: assignment.Instruction,
	})
	status := "completed"
	body := strings.TrimSpace(result.Output)
	errorText := ""
	if runErr != nil {
		status = "failed"
		errorText = runErr.Error()
		if body == "" {
			body = errorText
		}
	}
	return client.complete(ctx, agentID, assignment.TaskID, status, body, errorText)
}

type controlClient struct {
	base  string
	token string
	http  *http.Client
}

func (c controlClient) heartbeat(ctx context.Context, heartbeat remoteagent.Heartbeat) error {
	return c.do(ctx, http.MethodPost, "/agents/"+heartbeat.ID+"/heartbeat", heartbeat, nil)
}

func (c controlClient) claim(ctx context.Context, agentID, backend string) (*remoteagent.Assignment, error) {
	var out struct {
		Assignment *remoteagent.Assignment `json:"assignment"`
	}
	if err := c.do(ctx, http.MethodPost, "/agents/"+agentID+"/claim", map[string]any{"backend": backend}, &out); err != nil {
		return nil, err
	}
	return out.Assignment, nil
}

func (c controlClient) complete(ctx context.Context, agentID, taskID, status, result, errorText string) error {
	return c.do(ctx, http.MethodPost, "/agents/"+agentID+"/tasks/"+taskID+"/complete", map[string]any{
		"status": status,
		"result": result,
		"error":  errorText,
	}, nil)
}

func (c controlClient) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}
	if out == nil || len(respBody) == 0 {
		return nil
	}
	return json.Unmarshal(respBody, out)
}

func remoteWorkdirs(values []config.RemoteAgentWorkdirConfig) []remoteagent.Workdir {
	out := make([]remoteagent.Workdir, 0, len(values))
	for _, value := range values {
		path := strings.TrimSpace(value.Path)
		if path == "" {
			continue
		}
		id := strings.TrimSpace(value.ID)
		if id == "" {
			id = path
		}
		out = append(out, remoteagent.Workdir{ID: id, Path: path, Label: strings.TrimSpace(value.Label)})
	}
	return out
}

func remoteAgentMetadata(backend, terminalBaseURL string) ([]string, map[string]string) {
	capabilities := []string{"task.claim", "task.complete", "directory-context", backend}
	metadata := map[string]string{}
	if terminalBaseURL != "" {
		capabilities = append(capabilities, "terminal")
		metadata["terminal_base_url"] = terminalBaseURL
	}
	return capabilities, metadata
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "homelab-agent:", err)
	os.Exit(1)
}
