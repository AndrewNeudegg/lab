package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/andrewneudegg/lab/pkg/agent"
	"github.com/andrewneudegg/lab/pkg/chat"
	"github.com/andrewneudegg/lab/pkg/config"
	"github.com/andrewneudegg/lab/pkg/eventlog"
	"github.com/andrewneudegg/lab/pkg/llm"
	memstore "github.com/andrewneudegg/lab/pkg/memory"
	taskstore "github.com/andrewneudegg/lab/pkg/task"
	"github.com/andrewneudegg/lab/pkg/tool"
	approvalstore "github.com/andrewneudegg/lab/pkg/tools/approval"
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
	mode := flag.String("mode", "stdio", "adapter mode: stdio or webhook")
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
		adapter := chat.Webhook{Addr: cfg.HTTP.Addr, Handle: func(ctx context.Context, msg chat.ChatMessage) (string, error) {
			return orch.Handle(ctx, msg.From, msg.Content)
		}}
		fmt.Fprintf(os.Stdout, "homelabd webhook listening on %s\n", cfg.HTTP.Addr)
		if err := adapter.Listen(ctx); err != nil {
			fatal(err)
		}
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
		return llm.NewGemini(providerCfg.BaseURL, providerCfg.APIKey), providerCfg.Model, nil
	case "openai-compatible", "":
		return llm.NewOpenAICompatible(cfg.DefaultProvider, providerCfg.BaseURL, providerCfg.APIKey), providerCfg.Model, nil
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

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "homelabd:", err)
	os.Exit(1)
}
