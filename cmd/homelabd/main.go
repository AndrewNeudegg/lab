package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/andrewneudegg/lab/pkg/agent"
	"github.com/andrewneudegg/lab/pkg/chat"
	"github.com/andrewneudegg/lab/pkg/config"
	"github.com/andrewneudegg/lab/pkg/control"
	"github.com/andrewneudegg/lab/pkg/eventlog"
	agentrunner "github.com/andrewneudegg/lab/pkg/externalagent"
	"github.com/andrewneudegg/lab/pkg/llm"
	memstore "github.com/andrewneudegg/lab/pkg/memory"
	taskstore "github.com/andrewneudegg/lab/pkg/task"
	"github.com/andrewneudegg/lab/pkg/tool"
	approvalstore "github.com/andrewneudegg/lab/pkg/tools/approval"
	externalagenttools "github.com/andrewneudegg/lab/pkg/tools/externalagent"
	gittools "github.com/andrewneudegg/lab/pkg/tools/git"
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
	orch, err := buildRuntime(cfg)
	if err != nil {
		fatal(err)
	}

	switch *mode {
	case "stdio":
		runStdio(ctx, orch)
	case "webhook":
		adapter := chat.Webhook{Addr: cfg.HTTP.Addr, Handle: control.ChatHandler(orch)}
		fmt.Fprintf(os.Stdout, "homelabd webhook listening on %s\n", cfg.HTTP.Addr)
		if err := adapter.Listen(ctx); err != nil {
			fatal(err)
		}
	case "http":
		server := control.Server{Addr: cfg.HTTP.Addr, Orchestrator: orch}
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
	if err := testtools.Register(registry, testtools.Base{Timeout: timeout}); err != nil {
		return nil, err
	}
	if err := shelltools.Register(registry, shelltools.Base{Timeout: timeout}); err != nil {
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
	return agent.NewOrchestrator(cfg, events, tasks, approvals, registry, tool.NewPolicy(cfg.Policy.RequireApprovalFor), provider, model), nil
}

func buildProvider(cfg config.Config) (llm.Provider, string, error) {
	providerCfg, ok := cfg.Providers[cfg.DefaultProvider]
	if !ok {
		return nil, "", fmt.Errorf("default provider %q not configured", cfg.DefaultProvider)
	}
	switch providerCfg.Type {
	case "gemini":
		return llm.WithRetry(llm.NewGemini(providerCfg.BaseURL, providerCfg.APIKey), llm.RetryConfig{}), providerCfg.Model, nil
	case "openai-compatible", "":
		return llm.WithRetry(llm.NewOpenAICompatible(cfg.DefaultProvider, providerCfg.BaseURL, providerCfg.APIKey), llm.RetryConfig{}), providerCfg.Model, nil
	default:
		return nil, "", fmt.Errorf("unsupported provider type %q", providerCfg.Type)
	}
}

func runStdio(ctx context.Context, orch *agent.Orchestrator) {
	adapter := chat.Stdio{In: os.Stdin, Out: os.Stdout}
	msgs, err := adapter.Receive(ctx)
	if err != nil {
		fatal(err)
	}
	_ = adapter.Send(ctx, chat.OutboundMessage{Content: "homelabd ready. Type `help`."})
	for msg := range msgs {
		reply, err := orch.Handle(ctx, msg.From, msg.Content)
		if err != nil {
			reply = "error: " + err.Error()
		}
		_ = adapter.Send(ctx, chat.OutboundMessage{Content: reply})
	}
}

func runMatrix(ctx context.Context, cfg config.Config, orch *agent.Orchestrator) {
	errCh := make(chan error, 1)
	go func() {
		server := control.Server{Addr: cfg.HTTP.Addr, Orchestrator: orch}
		fmt.Fprintf(os.Stdout, "homelabd http listening on %s\n", cfg.HTTP.Addr)
		errCh <- server.Listen(ctx)
	}()
	adapter := chat.NewMatrix(chat.MatrixConfig{
		Homeserver:    cfg.Matrix.Homeserver,
		User:          cfg.Matrix.User,
		Password:      cfg.Matrix.Password,
		AccessToken:   cfg.Matrix.AccessToken,
		RoomID:        cfg.Matrix.RoomID,
		RoomAlias:     cfg.Matrix.RoomAlias,
		RoomName:      cfg.Matrix.RoomName,
		SyncTimeoutMS: cfg.Matrix.SyncTimeoutMS,
	})
	msgs, err := adapter.Receive(ctx)
	if err != nil {
		fatal(err)
	}
	fmt.Fprintf(os.Stdout, "homelabd matrix listening for room %q (%s)\n", cfg.Matrix.RoomName, adapter.RoomID())
	for {
		select {
		case err := <-errCh:
			if err != nil {
				fmt.Fprintln(os.Stderr, "homelabd http sidecar:", err)
			}
		case msg, ok := <-msgs:
			if !ok {
				return
			}
			fmt.Fprintf(os.Stdout, "homelabd matrix received from %s: %s\n", msg.From, msg.Content)
			content, addressed := matrixAddressedContent(cfg, msg.Content)
			if !addressed {
				continue
			}
			reply, err := orch.Handle(ctx, msg.From, content)
			if err != nil {
				reply = "error: " + err.Error()
			}
			if err := adapter.Send(ctx, chat.OutboundMessage{Content: reply}); err != nil {
				fmt.Fprintln(os.Stderr, "homelabd matrix send:", err)
			}
		}
	}
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

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "homelabd:", err)
	os.Exit(1)
}
