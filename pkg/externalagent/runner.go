package externalagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/andrewneudegg/lab/pkg/config"
	"github.com/andrewneudegg/lab/pkg/id"
)

type Runner struct {
	agents map[string]config.ExternalAgentConfig
}

type Agent struct {
	Name        string   `json:"name"`
	Enabled     bool     `json:"enabled"`
	Available   bool     `json:"available"`
	Command     string   `json:"command,omitempty"`
	Args        []string `json:"args,omitempty"`
	Description string   `json:"description,omitempty"`
}

type RunRequest struct {
	Backend     string `json:"backend"`
	TaskID      string `json:"task_id"`
	Workspace   string `json:"workspace"`
	Instruction string `json:"instruction"`
}

type RunResult struct {
	ID         string        `json:"id"`
	Backend    string        `json:"backend"`
	TaskID     string        `json:"task_id"`
	Workspace  string        `json:"workspace"`
	Command    []string      `json:"command"`
	Output     string        `json:"output"`
	Error      string        `json:"error,omitempty"`
	Duration   time.Duration `json:"duration"`
	StartedAt  time.Time     `json:"started_at"`
	FinishedAt time.Time     `json:"finished_at"`
}

func NewRunner(agents map[string]config.ExternalAgentConfig) *Runner {
	return &Runner{agents: agents}
}

func (r *Runner) List() []Agent {
	agents := make([]Agent, 0, len(r.agents))
	for name, cfg := range r.agents {
		agents = append(agents, Agent{
			Name:        name,
			Enabled:     cfg.Enabled,
			Available:   cfg.Enabled && cfg.Command != "" && commandAvailable(cfg.Command),
			Command:     cfg.Command,
			Args:        redactArgs(cfg.Args),
			Description: cfg.Description,
		})
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i].Name < agents[j].Name })
	return agents
}

func (r *Runner) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	cfg, ok := r.agents[req.Backend]
	if !ok {
		return RunResult{}, fmt.Errorf("external agent %q not configured", req.Backend)
	}
	if !cfg.Enabled {
		return RunResult{}, fmt.Errorf("external agent %q is disabled", req.Backend)
	}
	if cfg.Command == "" {
		return RunResult{}, fmt.Errorf("external agent %q command is not configured", req.Backend)
	}
	if req.Workspace == "" {
		return RunResult{}, fmt.Errorf("workspace is required")
	}
	if strings.TrimSpace(req.Instruction) == "" {
		return RunResult{}, fmt.Errorf("instruction is required")
	}
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	childCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	started := time.Now().UTC()
	args := append([]string{}, cfg.Args...)
	cmd := exec.CommandContext(childCtx, cfg.Command, args...)
	cmd.Dir = req.Workspace
	cmd.Env = append(cmd.Environ(),
		"HOMELABD_TASK_ID="+req.TaskID,
		"HOMELABD_WORKSPACE="+req.Workspace,
		"HOMELABD_BACKEND="+req.Backend,
	)
	cmd.Stdin = strings.NewReader(req.Instruction)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	finished := time.Now().UTC()
	result := RunResult{
		ID:         id.New("external_run"),
		Backend:    req.Backend,
		TaskID:     req.TaskID,
		Workspace:  req.Workspace,
		Command:    append([]string{cfg.Command}, redactArgs(args)...),
		Output:     out.String(),
		Duration:   finished.Sub(started),
		StartedAt:  started,
		FinishedAt: finished,
	}
	if childCtx.Err() == context.DeadlineExceeded {
		result.Error = "external agent timed out"
		return result, childCtx.Err()
	}
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	return result, nil
}

func (r *Runner) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.List())
}

func commandAvailable(command string) bool {
	if strings.Contains(command, "/") {
		_, err := exec.LookPath(command)
		return err == nil
	}
	_, err := exec.LookPath(command)
	return err == nil
}

func redactArgs(args []string) []string {
	out := append([]string{}, args...)
	for i, arg := range out {
		lower := strings.ToLower(arg)
		if strings.Contains(lower, "key") || strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") {
			if strings.Contains(arg, "=") {
				name, _, _ := strings.Cut(arg, "=")
				out[i] = name + "=<redacted>"
				continue
			}
			out[i] = "<redacted>"
			if i+1 < len(out) && !strings.HasPrefix(out[i+1], "-") {
				out[i+1] = "<redacted>"
			}
		}
	}
	return out
}
