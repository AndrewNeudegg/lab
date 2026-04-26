package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/andrewneudegg/lab/pkg/agent"
	"github.com/andrewneudegg/lab/pkg/chat"
	"github.com/andrewneudegg/lab/pkg/config"
	"github.com/andrewneudegg/lab/pkg/control"
	"github.com/andrewneudegg/lab/pkg/eventlog"
	agentrunner "github.com/andrewneudegg/lab/pkg/externalagent"
	"github.com/andrewneudegg/lab/pkg/healthd"
	"github.com/andrewneudegg/lab/pkg/llm"
	memstore "github.com/andrewneudegg/lab/pkg/memory"
	taskstore "github.com/andrewneudegg/lab/pkg/task"
	"github.com/andrewneudegg/lab/pkg/tool"
	approvalstore "github.com/andrewneudegg/lab/pkg/tools/approval"
	externalagenttools "github.com/andrewneudegg/lab/pkg/tools/externalagent"
	gittools "github.com/andrewneudegg/lab/pkg/tools/git"
	internettools "github.com/andrewneudegg/lab/pkg/tools/internet"
	memtools "github.com/andrewneudegg/lab/pkg/tools/memory"
	repotools "github.com/andrewneudegg/lab/pkg/tools/repo"
	shelltools "github.com/andrewneudegg/lab/pkg/tools/shell"
	supervisortools "github.com/andrewneudegg/lab/pkg/tools/supervisor"
	tasktools "github.com/andrewneudegg/lab/pkg/tools/task"
	testtools "github.com/andrewneudegg/lab/pkg/tools/test"
)

func main() {
	configPath := flag.String("config", "config.json", "configuration file")
	mode := flag.String("mode", "stdio", "adapter mode: stdio, webhook, http, or matrix")
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
	startedAt := time.Now().UTC()
	startHealthdHeartbeat(ctx, cfg, *mode, startedAt)
	orch, err := buildRuntime(cfg)
	if err != nil {
		fatal(err)
	}
	if _, err := orch.RecoverRunningTasks(ctx); err != nil {
		fatal(err)
	}
	orch.StartTaskSupervisor(ctx)
	switch *mode {
	case "stdio":
		runStdio(ctx, cfg, orch)
	case "webhook":
		adapter := chat.Webhook{Addr: cfg.HTTP.Addr, Handle: control.ChatHandler(orch)}
		fmt.Fprintf(os.Stdout, "homelabd webhook listening on %s\n", cfg.HTTP.Addr)
		if err := adapter.Listen(ctx); err != nil {
			fatal(err)
		}
	case "http":
		server := control.Server{Addr: cfg.HTTP.Addr, Orchestrator: orch, ChatLogDir: filepath.Join(cfg.DataDir, "chat")}
		fmt.Fprintf(os.Stdout, "homelabd http listening on %s\n", cfg.HTTP.Addr)
		if err := server.Listen(ctx); err != nil {
			fatal(err)
		}
	case "matrix":
		runMatrix(ctx, cfg, orch)
	default:
		fatal(fmt.Errorf("unknown mode %q", *mode))
	}
}

func startHealthdHeartbeat(ctx context.Context, cfg config.Config, mode string, startedAt time.Time) {
	if cfg.Healthd.Enabled != nil && !*cfg.Healthd.Enabled {
		return
	}
	interval := time.Duration(cfg.Healthd.ProcessHeartbeatIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	timeout := time.Duration(cfg.Healthd.RequestTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	send := func() {
		heartbeat := healthd.ProcessHeartbeat{
			Name:       "homelabd",
			Type:       "daemon",
			PID:        os.Getpid(),
			Addr:       cfg.HTTP.Addr,
			StartedAt:  startedAt,
			TTLSeconds: cfg.Healthd.ProcessTimeoutSeconds,
			Metadata: map[string]string{
				"mode":  mode,
				"agent": cfg.AgentName,
			},
		}
		if err := healthd.PushHeartbeat(ctx, client, cfg.Healthd.Addr, heartbeat); err != nil {
			slog.Warn("healthd heartbeat failed", "error", err)
		}
	}
	go func() {
		send()
		ticker := time.NewTicker(interval)
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
}

func buildRuntime(cfg config.Config) (*agent.Orchestrator, error) {
	registry := tool.NewRegistry()
	timeout := time.Duration(cfg.Limits.MaxShellSeconds) * time.Second
	tasks := taskstore.NewStore(filepath.Join(cfg.DataDir, "tasks"))
	if err := repotools.Register(registry, repotools.Base{Root: cfg.Repo.Root, WorkspaceRoot: cfg.Repo.WorkspaceRoot, MaxFileBytes: cfg.Limits.MaxFileBytes}); err != nil {
		return nil, err
	}
	if err := gittools.Register(registry, gittools.Base{RepoRoot: cfg.Repo.Root, WorkspaceRoot: cfg.Repo.WorkspaceRoot}); err != nil {
		return nil, err
	}
	if err := testtools.Register(registry, testtools.Base{Timeout: timeout, RepoRoot: cfg.Repo.Root}); err != nil {
		return nil, err
	}
	if err := shelltools.Register(registry, shelltools.Base{Timeout: timeout}); err != nil {
		return nil, err
	}
	if err := internettools.Register(registry, internettools.Base{}); err != nil {
		return nil, err
	}
	if err := memtools.Register(registry, memstore.NewStore("memory")); err != nil {
		return nil, err
	}
	if err := supervisortools.Register(registry); err != nil {
		return nil, err
	}
	if err := tasktools.Register(registry, tasks); err != nil {
		return nil, err
	}
	if err := externalagenttools.Register(registry, agentrunner.NewRunner(cfg.ExternalAgents)); err != nil {
		return nil, err
	}
	events := eventlog.NewStore(filepath.Join(cfg.DataDir, "events"))
	approvals := approvalstore.NewStore(filepath.Join(cfg.DataDir, "approvals"))
	provider, model, err := buildProvider(cfg)
	if err != nil {
		return nil, err
	}
	return agent.NewOrchestrator(cfg, events, tasks, approvals, registry, tool.NewPolicy(cfg.Policy.RequireApprovalFor), provider, model).WithLogger(slog.Default()), nil
}

func buildProvider(cfg config.Config) (llm.Provider, string, error) {
	var candidates []llm.ProviderCandidate
	addCandidate := func(name string) error {
		providerCfg, ok := cfg.Providers[name]
		if !ok {
			return fmt.Errorf("provider %q not configured", name)
		}
		provider, model, err := buildSingleProvider(name, providerCfg)
		if err != nil {
			return err
		}
		candidates = append(candidates, llm.ProviderCandidate{Name: name, Model: model, Provider: provider})
		return nil
	}
	if err := addCandidate(cfg.DefaultProvider); err != nil {
		return nil, "", err
	}
	if cfg.DefaultProvider != "openai" {
		if providerCfg, ok := cfg.Providers["openai"]; ok && providerUsable(providerCfg) {
			provider, model, err := buildSingleProvider("openai", providerCfg)
			if err != nil {
				return nil, "", err
			}
			candidates = append(candidates, llm.ProviderCandidate{Name: "openai", Model: model, Provider: provider})
		}
	}
	return llm.NewFallbackProvider(candidates), candidates[0].Model, nil
}

func buildSingleProvider(name string, providerCfg config.ProviderConfig) (llm.Provider, string, error) {
	switch providerCfg.Type {
	case "gemini":
		return llm.WithRetry(llm.NewGemini(providerCfg.BaseURL, providerCfg.APIKey), llm.RetryConfig{}), providerCfg.Model, nil
	case "openai-compatible", "":
		return llm.WithRetry(llm.NewOpenAICompatible(name, providerCfg.BaseURL, providerCfg.APIKey), llm.RetryConfig{}), providerCfg.Model, nil
	default:
		return nil, "", fmt.Errorf("unsupported provider type %q", providerCfg.Type)
	}
}

func providerUsable(providerCfg config.ProviderConfig) bool {
	switch providerCfg.Type {
	case "gemini", "openai-compatible", "":
		return providerCfg.APIKey != "" || strings.Contains(providerCfg.BaseURL, "localhost") || strings.Contains(providerCfg.BaseURL, "127.0.0.1")
	default:
		return false
	}
}

func runStdio(ctx context.Context, cfg config.Config, orch *agent.Orchestrator) {
	adapter := chat.Stdio{In: os.Stdin, Out: os.Stdout}
	transcript := newChatTranscript(cfg)
	msgs, err := adapter.Receive(ctx)
	if err != nil {
		fatal(err)
	}
	ready := "homelabd ready. Type `help`."
	_ = transcript.Append("stdio", "out", "homelabd", "stdio", ready, false)
	_ = adapter.Send(ctx, chat.OutboundMessage{Content: ready})
	for msg := range msgs {
		logLine("stdio received from %s: %s", msg.From, msg.Content)
		_ = transcript.Append("stdio", "in", msg.From, "homelabd", msg.Content, true)
		reply, err := orch.Handle(ctx, msg.From, msg.Content)
		if err != nil {
			reply = "error: " + err.Error()
		}
		logLine("stdio reply to %s: %s", msg.From, oneLine(reply))
		_ = transcript.Append("stdio", "out", "homelabd", msg.From, reply, true)
		_ = adapter.Send(ctx, chat.OutboundMessage{Content: reply})
	}
}

func runMatrix(ctx context.Context, cfg config.Config, orch *agent.Orchestrator) {
	transcript := newChatTranscript(cfg)
	go runHTTPSidecar(ctx, cfg, orch)
	runMatrixLoop(ctx, cfg, orch, transcript)
}

func runHTTPSidecar(ctx context.Context, cfg config.Config, orch *agent.Orchestrator) {
	backoff := 2 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		server := control.Server{Addr: cfg.HTTP.Addr, Orchestrator: orch, ChatLogDir: filepath.Join(cfg.DataDir, "chat")}
		logLine("http listening on %s", cfg.HTTP.Addr)
		if err := server.Listen(ctx); err != nil && ctx.Err() == nil {
			slog.Error("homelabd http sidecar stopped; retrying", "error", err, "backoff", backoff)
			if !sleepContext(ctx, backoff) {
				return
			}
			backoff = minDuration(backoff*2, 30*time.Second)
			continue
		}
		return
	}
}

func runMatrixLoop(ctx context.Context, cfg config.Config, orch *agent.Orchestrator, transcript *chatTranscript) {
	backoff := 2 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		adapter := newMatrixAdapter(cfg)
		msgs, err := adapter.Receive(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("matrix adapter failed; retrying", "error", err, "backoff", backoff)
			if !sleepContext(ctx, backoff) {
				return
			}
			backoff = minDuration(backoff*2, 60*time.Second)
			continue
		}
		backoff = 2 * time.Second
		logLine("matrix listening for room %q (%s)", cfg.Matrix.RoomName, adapter.RoomID())
		if !handleMatrixMessages(ctx, cfg, orch, transcript, adapter, msgs) {
			return
		}
		slog.Warn("matrix message stream closed; reconnecting", "backoff", backoff)
		if !sleepContext(ctx, backoff) {
			return
		}
	}
}

func newMatrixAdapter(cfg config.Config) *chat.Matrix {
	return chat.NewMatrix(chat.MatrixConfig{
		Homeserver:    cfg.Matrix.Homeserver,
		User:          cfg.Matrix.User,
		Password:      cfg.Matrix.Password,
		AccessToken:   cfg.Matrix.AccessToken,
		RoomID:        cfg.Matrix.RoomID,
		RoomAlias:     cfg.Matrix.RoomAlias,
		RoomName:      cfg.Matrix.RoomName,
		SyncTimeoutMS: cfg.Matrix.SyncTimeoutMS,
	})
}

func handleMatrixMessages(ctx context.Context, cfg config.Config, orch *agent.Orchestrator, transcript *chatTranscript, adapter *chat.Matrix, msgs <-chan chat.ChatMessage) bool {
	for {
		select {
		case <-ctx.Done():
			return false
		case msg, ok := <-msgs:
			if !ok {
				return true
			}
			logLine("matrix received from %s: %s", msg.From, msg.Content)
			_ = transcript.Append("matrix", "in", msg.From, "homelabd", msg.Content, false)
			content, addressed := matrixAddressedContent(cfg, msg.Content)
			logLine("matrix addressed=%t content=%q", addressed, content)
			if !addressed {
				continue
			}
			if shouldAck(content) {
				ack := "ack: working on " + quoteForAck(content)
				_ = transcript.Append("matrix", "out", "homelabd", msg.From, ack, true)
				if err := adapter.Send(ctx, chat.OutboundMessage{Content: ack}); err != nil {
					fmt.Fprintln(os.Stderr, "homelabd matrix ack send:", err)
				}
			}
			reply, err := orch.Handle(ctx, msg.From, content)
			if err != nil {
				reply = "error: " + err.Error()
			}
			logLine("matrix reply to %s: %s", msg.From, oneLine(reply))
			_ = transcript.Append("matrix", "out", "homelabd", msg.From, reply, true)
			if err := adapter.Send(ctx, chat.OutboundMessage{Content: reply}); err != nil {
				fmt.Fprintln(os.Stderr, "homelabd matrix send:", err)
			}
		}
	}
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func matrixAddressedContent(cfg config.Config, content string) (string, bool) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "", false
	}
	if !cfg.Matrix.RequirePrefix {
		return trimmed, true
	}
	prefixes := []string{cfg.Matrix.Prefix}
	if cfg.Matrix.User != "" {
		prefixes = append(prefixes, cfg.Matrix.User, strings.TrimPrefix(cfg.Matrix.User, "@"))
	}
	prefixes = append(prefixes, "element-bot")
	for _, prefix := range prefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" {
			continue
		}
		if strings.EqualFold(trimmed, prefix) {
			return "help", true
		}
		for _, sep := range []string{" ", ":", ","} {
			candidate := prefix + sep
			if len(trimmed) >= len(candidate) && strings.EqualFold(trimmed[:len(candidate)], candidate) {
				rest := strings.TrimSpace(trimmed[len(candidate):])
				if rest == "" {
					rest = "help"
				}
				return rest, true
			}
		}
	}
	return "", false
}

type chatTranscript struct {
	dir string
	mu  sync.Mutex
}

func newChatTranscript(cfg config.Config) *chatTranscript {
	return &chatTranscript{dir: filepath.Join(cfg.DataDir, "chat")}
}

func (t *chatTranscript) Append(adapter, direction, from, to, content string, addressed bool) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := os.MkdirAll(t.dir, 0o755); err != nil {
		return err
	}
	record := map[string]any{
		"time":      time.Now().UTC(),
		"adapter":   adapter,
		"direction": direction,
		"from":      from,
		"to":        to,
		"content":   content,
		"addressed": addressed,
	}
	b, err := json.Marshal(record)
	if err != nil {
		return err
	}
	path := filepath.Join(t.dir, time.Now().UTC().Format("2006-01-02")+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

func logLine(format string, args ...any) {
	slog.Info(fmt.Sprintf(format, args...))
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 240 {
		return s[:240] + "..."
	}
	return s
}

func shouldAck(content string) bool {
	fields := strings.Fields(strings.ToLower(content))
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "new", "task", "run", "work", "start", "delegate", "escalate", "codex", "claude", "gemini", "review", "approve", "delete", "remove", "cancel", "refresh", "rebase", "sync":
		return true
	default:
		return false
	}
}

func quoteForAck(content string) string {
	content = strings.TrimSpace(content)
	if len(content) > 80 {
		content = content[:80] + "..."
	}
	return strconvQuote(content)
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func fatal(err error) {
	slog.Error("homelabd fatal", "error", err)
	fmt.Fprintln(os.Stderr, "homelabd:", err)
	os.Exit(1)
}
