package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/andrewneudegg/lab/pkg/config"
	controlserver "github.com/andrewneudegg/lab/pkg/control"
	agentrunner "github.com/andrewneudegg/lab/pkg/externalagent"
	"github.com/andrewneudegg/lab/pkg/remoteagent"
	taskstore "github.com/andrewneudegg/lab/pkg/task"
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
			agentCfg.Workdirs = append(agentCfg.Workdirs, config.RemoteAgentWorkdirConfig{ID: workdir.ID, Path: workdir.Path, Label: workdir.Label, ProjectID: workdir.ProjectID, RepoURL: workdir.RepoURL, Branch: workdir.Branch, Labels: workdir.Labels, Metadata: workdir.Metadata})
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
	state := newRemoteAgentRuntimeState()
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
			CurrentTaskID: state.currentTask(),
			Metadata:      metadata,
		})
		if err != nil {
			slog.Warn("remote heartbeat failed", "error", err)
		}
	}
	heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
	heartbeatDone := startRemoteHeartbeatLoop(heartbeatCtx, heartbeatEvery, sendHeartbeat)
	defer func() {
		stopHeartbeat()
		<-heartbeatDone
	}()
	pollTicker := time.NewTicker(pollEvery)
	defer pollTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-pollTicker.C:
			if state.currentTask() != "" {
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
			state.setCurrentTask(assignment.TaskID)
			sendHeartbeat()
			slog.Info("remote assignment claimed", "task_id", assignment.TaskID, "workdir", assignment.Workdir, "backend", assignment.Backend)
			if err := executeAssignment(ctx, client, runner, agentCfg.ID, agentCfg.Backend, assignment); err != nil {
				slog.Warn("remote completion failed", "task_id", assignment.TaskID, "error", err)
			}
			state.setCurrentTask("")
			sendHeartbeat()
		}
	}
}

type remoteAgentRuntimeState struct {
	currentTaskID atomic.Value
}

func newRemoteAgentRuntimeState() *remoteAgentRuntimeState {
	state := &remoteAgentRuntimeState{}
	state.currentTaskID.Store("")
	return state
}

func (s *remoteAgentRuntimeState) setCurrentTask(taskID string) {
	s.currentTaskID.Store(strings.TrimSpace(taskID))
}

func (s *remoteAgentRuntimeState) currentTask() string {
	value, _ := s.currentTaskID.Load().(string)
	return value
}

func startRemoteHeartbeatLoop(ctx context.Context, every time.Duration, send func()) <-chan struct{} {
	done := make(chan struct{})
	send()
	go func() {
		defer close(done)
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				send()
			}
		}
	}()
	return done
}

type agentControl interface {
	complete(ctx context.Context, agentID, taskID, status, result, errorText string, diff gitDiffSnapshot) error
}

type assignmentRunner interface {
	Run(ctx context.Context, req agentrunner.RunRequest) (agentrunner.RunResult, error)
}

func executeAssignment(ctx context.Context, client agentControl, runner assignmentRunner, agentID, fallbackBackend string, assignment *remoteagent.Assignment) error {
	if assignment == nil {
		return nil
	}
	baseline, baselineErr := captureGitTreeSnapshot(ctx, assignment.Workdir)
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
		errorText = firstNonEmpty(result.Error, runErr.Error())
		if errors.Is(runErr, context.DeadlineExceeded) {
			status = taskstore.StatusTimedOut
		}
		if body == "" {
			body = errorText
		}
	} else if workerReportedNoChangeRequired(body) {
		status = "no_change_required"
	}
	diff, diffErr := captureGitDiff(ctx, assignment.Workdir, baseline)
	if baselineErr != nil {
		body = appendResultNote(body, "Remote diff baseline capture failed: "+baselineErr.Error())
	}
	if diffErr != nil {
		body = appendResultNote(body, "Remote diff capture failed: "+diffErr.Error())
	}
	return client.complete(ctx, agentID, assignment.TaskID, status, body, errorText, diff)
}

type gitTreeSnapshot struct {
	Tree string
}

type gitDiffSnapshot struct {
	RawDiff string
	Source  string
	BaseRef string
	HeadRef string
	Warning string
}

func captureGitTreeSnapshot(ctx context.Context, workdir string) (gitTreeSnapshot, error) {
	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		return gitTreeSnapshot{}, nil
	}
	if _, err := os.Stat(filepath.Join(workdir, ".git")); err != nil {
		return gitTreeSnapshot{}, nil
	}
	tree, err := worktreeTree(ctx, workdir)
	if err != nil {
		return gitTreeSnapshot{}, err
	}
	return gitTreeSnapshot{Tree: tree}, nil
}

func captureGitDiff(ctx context.Context, workdir string, baseline gitTreeSnapshot) (gitDiffSnapshot, error) {
	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		return gitDiffSnapshot{}, nil
	}
	if _, err := os.Stat(filepath.Join(workdir, ".git")); err != nil {
		return gitDiffSnapshot{}, nil
	}
	if strings.TrimSpace(baseline.Tree) != "" {
		headTree, err := worktreeTree(ctx, workdir)
		if err != nil {
			return gitDiffSnapshot{}, err
		}
		diffOut, err := exec.CommandContext(ctx, "git", "-C", workdir, "diff", "--binary", baseline.Tree, headTree, "--", ".").CombinedOutput()
		if err != nil {
			return gitDiffSnapshot{}, fmt.Errorf("git diff task snapshot: %w: %s", err, strings.TrimSpace(string(diffOut)))
		}
		return gitDiffSnapshot{
			RawDiff: string(diffOut),
			Source:  "remote_agent_task_snapshot",
			BaseRef: baseline.Tree,
			HeadRef: headTree,
		}, nil
	}
	diff, err := remoteWorkingTreeDiff(ctx, workdir)
	return gitDiffSnapshot{
		RawDiff: diff,
		Source:  "remote_live_worktree_fallback",
		Warning: "Remote agent could not create a task baseline, so this diff was generated from the current checkout and may include unrelated work.",
	}, err
}

func worktreeTree(ctx context.Context, workdir string) (string, error) {
	indexFile, err := os.CreateTemp("", "homelab-agent-index-*")
	if err != nil {
		return "", err
	}
	indexPath := indexFile.Name()
	_ = indexFile.Close()
	defer os.Remove(indexPath)
	env := append(os.Environ(), "GIT_INDEX_FILE="+indexPath)
	if out, err := gitCommand(ctx, workdir, env, nil, "read-tree", "HEAD"); err != nil {
		if _, emptyErr := gitCommand(ctx, workdir, env, nil, "read-tree", "--empty"); emptyErr != nil {
			return "", fmt.Errorf("git read-tree HEAD: %w: %s", err, strings.TrimSpace(out))
		}
	}
	pathspecs, err := gitCommand(ctx, workdir, nil, nil, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return "", fmt.Errorf("git ls-files snapshot: %w: %s", err, strings.TrimSpace(pathspecs))
	}
	filtered := filterSnapshotPathspecs(pathspecs)
	if len(filtered) > 0 {
		if out, err := gitCommand(ctx, workdir, env, strings.NewReader(string(filtered)), "add", "-A", "--pathspec-from-file=-", "--pathspec-file-nul"); err != nil {
			return "", fmt.Errorf("git add snapshot: %w: %s", err, strings.TrimSpace(out))
		}
	}
	tree, err := gitCommand(ctx, workdir, env, nil, "write-tree")
	if err != nil {
		return "", fmt.Errorf("git write-tree snapshot: %w: %s", err, strings.TrimSpace(tree))
	}
	return strings.TrimSpace(tree), nil
}

func gitCommand(ctx context.Context, workdir string, env []string, stdin io.Reader, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", workdir}, args...)...)
	if env != nil {
		cmd.Env = env
	}
	cmd.Stdin = stdin
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func filterSnapshotPathspecs(raw string) []byte {
	var b strings.Builder
	for _, rel := range strings.Split(raw, "\x00") {
		rel = strings.TrimSpace(rel)
		if skipRemoteUntrackedDiffPath(rel) {
			continue
		}
		b.WriteString(rel)
		b.WriteByte(0)
	}
	return []byte(b.String())
}

func remoteWorkingTreeDiff(ctx context.Context, workdir string) (string, error) {
	diffOut, err := exec.CommandContext(ctx, "git", "-C", workdir, "diff", "--binary", "HEAD", "--", ".").CombinedOutput()
	if err != nil {
		diffOut, err = exec.CommandContext(ctx, "git", "-C", workdir, "diff", "--binary", "--", ".").CombinedOutput()
	}
	if err != nil {
		return "", fmt.Errorf("git diff: %w: %s", err, strings.TrimSpace(string(diffOut)))
	}
	var b strings.Builder
	b.Write(diffOut)
	untrackedOut, err := exec.CommandContext(ctx, "git", "-C", workdir, "ls-files", "--others", "--exclude-standard").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git ls-files untracked: %w: %s", err, strings.TrimSpace(string(untrackedOut)))
	}
	for _, rel := range strings.Split(string(untrackedOut), "\n") {
		rel = strings.TrimSpace(rel)
		if skipRemoteUntrackedDiffPath(rel) {
			continue
		}
		out, diffErr := exec.CommandContext(ctx, "git", "-C", workdir, "diff", "--no-index", "--binary", "--", "/dev/null", rel).CombinedOutput()
		if diffErr != nil {
			var exitErr *exec.ExitError
			if !errors.As(diffErr, &exitErr) || exitErr.ExitCode() != 1 {
				return "", fmt.Errorf("git diff untracked %s: %w: %s", rel, diffErr, strings.TrimSpace(string(out)))
			}
		}
		b.Write(out)
	}
	return b.String(), nil
}

func skipRemoteUntrackedDiffPath(rel string) bool {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	return rel == "" ||
		rel == ".codex" ||
		strings.HasPrefix(rel, ".codex/") ||
		strings.HasPrefix(rel, ".agent-")
}

func appendResultNote(body, note string) string {
	body = strings.TrimSpace(body)
	note = strings.TrimSpace(note)
	if note == "" {
		return body
	}
	if body == "" {
		return note
	}
	return body + "\n\n" + note
}

func workerReportedNoChangeRequired(result string) bool {
	for _, line := range strings.Split(result, "\n") {
		normalized := strings.ToLower(strings.TrimSpace(line))
		normalized = strings.TrimLeft(normalized, "-*#> ")
		normalized = strings.ReplaceAll(normalized, "**", "")
		normalized = strings.Trim(normalized, "*_` ")
		if normalized == "no change required" ||
			strings.HasPrefix(normalized, "no change required:") ||
			strings.HasPrefix(normalized, "no change required -") ||
			normalized == "no changes required" ||
			strings.HasPrefix(normalized, "no changes required:") ||
			strings.HasPrefix(normalized, "no changes required -") {
			return true
		}
	}
	return false
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

func (c controlClient) complete(ctx context.Context, agentID, taskID, status, result, errorText string, diff gitDiffSnapshot) error {
	return c.do(ctx, http.MethodPost, "/agents/"+agentID+"/tasks/"+taskID+"/complete", map[string]any{
		"status":        status,
		"result":        result,
		"error":         errorText,
		"diff":          diff.RawDiff,
		"diff_source":   diff.Source,
		"diff_base_ref": diff.BaseRef,
		"diff_head_ref": diff.HeadRef,
		"diff_warning":  diff.Warning,
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
		out = append(out, remoteagent.Workdir{
			ID:        id,
			Path:      path,
			Label:     strings.TrimSpace(value.Label),
			ProjectID: strings.TrimSpace(value.ProjectID),
			RepoURL:   strings.TrimSpace(value.RepoURL),
			Branch:    strings.TrimSpace(value.Branch),
			Labels:    compactRemoteLabels(value.Labels),
			Metadata:  compactRemoteMetadata(value.Metadata),
		})
	}
	return out
}

func compactRemoteLabels(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func compactRemoteMetadata(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := map[string]string{}
	for key, value := range values {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
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
