package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAddr            = "http://127.0.0.1:18080"
	defaultHealthdAddr     = "http://127.0.0.1:18081"
	defaultSupervisordAddr = "http://127.0.0.1:18082"
	defaultFrom            = "homelabctl"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv, nil))
}

type cli struct {
	base            string
	healthdBase     string
	supervisordBase string
	http            *http.Client
	in              io.Reader
	out             io.Writer
	err             io.Writer
	from            string
	json            bool
}

type stringListFlag []string

func (f *stringListFlag) String() string {
	if f == nil {
		return ""
	}
	return strings.Join(*f, ",")
}

func (f *stringListFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value != "" {
		*f = append(*f, value)
	}
	return nil
}

func run(args []string, in io.Reader, out, errOut io.Writer, getenv func(string) string, httpClient *http.Client) int {
	if getenv == nil {
		getenv = os.Getenv
	}
	flags := flag.NewFlagSet("homelabctl", flag.ContinueOnError)
	flags.SetOutput(errOut)
	addr := flags.String("addr", envDefault(getenv, "HOMELABD_ADDR", defaultAddr), "homelabd base URL")
	healthdAddr := flags.String("healthd-addr", envDefault(getenv, "HOMELABD_HEALTHD_ADDR", defaultHealthdAddr), "healthd base URL")
	supervisordAddr := flags.String("supervisord-addr", envDefault(getenv, "HOMELABD_SUPERVISORD_ADDR", defaultSupervisordAddr), "supervisord base URL")
	from := flags.String("from", envDefault(getenv, "HOMELABCTL_FROM", defaultFrom), "sender name for chat messages")
	timeout := flags.Duration("timeout", 30*time.Second, "HTTP request timeout")
	jsonOutput := flags.Bool("json", false, "print the full JSON response for chat commands")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	rest := flags.Args()
	if len(rest) == 0 {
		usage(errOut)
		return 2
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: *timeout}
	} else if httpClient.Timeout == 0 && *timeout > 0 {
		copy := *httpClient
		copy.Timeout = *timeout
		httpClient = &copy
	}
	c := cli{
		base:            strings.TrimRight(*addr, "/"),
		healthdBase:     strings.TrimRight(*healthdAddr, "/"),
		supervisordBase: strings.TrimRight(*supervisordAddr, "/"),
		http:            httpClient,
		in:              in,
		out:             out,
		err:             errOut,
		from:            *from,
		json:            *jsonOutput,
	}
	if err := c.dispatch(rest); err != nil {
		fmt.Fprintln(errOut, "homelabctl:", err)
		return 1
	}
	return 0
}

func (c cli) dispatch(args []string) error {
	cmd := commandWord(args[0])
	switch cmd {
	case "help", "-h", "--help":
		usage(c.out)
		return nil
	case "health", "healthz":
		return c.do(http.MethodGet, "/healthz", nil)
	case "message", "say", "send":
		return c.message(strings.Join(args[1:], " "))
	case "chat":
		return c.chat(args[1:])
	case "shell", "interactive", "repl":
		return c.shell()
	case "task":
		return c.task(args[1:])
	case "knowledge", "knowledge-space", "kspace":
		return c.knowledge(args[1:])
	case "workflow":
		return c.workflow(args[1:])
	case "workflows":
		if len(args) == 1 {
			return c.workflow([]string{"list"})
		}
		return c.workflow(args[1:])
	case "settings", "setting":
		return c.settings(args[1:])
	case "auto-merge", "automerge":
		return c.settings(withAction("auto-merge", args[1:]))
	case "assistant", "assist":
		return c.assistant(args[1:])
	case "goal":
		return c.goal(args[1:])
	case "goals":
		if len(args) == 1 {
			return c.goal([]string{"list"})
		}
		return c.goal(args[1:])
	case "agent":
		return c.agent(args[1:])
	case "tasks":
		if len(args) == 1 {
			return c.task([]string{"list"})
		}
		return c.task(args[1:])
	case "approval":
		return c.approval(args[1:])
	case "approvals":
		if len(args) == 1 {
			return c.approval([]string{"list"})
		}
		return c.approval(args[1:])
	case "approve", "deny":
		return c.approval(args)
	case "events", "event":
		return c.events(args[1:])
	case "workspace", "workspaces", "project", "projects":
		return c.workspace(args[1:])
	case "healthd":
		return c.healthd(args[1:])
	case "errors", "error":
		return c.healthd(withAction("errors", args[1:]))
	case "supervisor", "supervisord":
		return c.supervisor(args[1:])
	case "terminal", "term":
		return c.terminal(args[1:])
	case "new":
		return c.task(withAction("new", args[1:]))
	case "show":
		return c.task(withAction("show", args[1:]))
	case "runs":
		return c.task(withAction("runs", args[1:]))
	case "diff":
		return c.task(withAction("diff", args[1:]))
	case "run", "review", "review-ui", "queue", "merge-queue", "accept", "verify", "restart", "reopen", "cancel", "stop", "retry", "delete", "remove", "rm":
		return c.task(withAction(cmd, args[1:]))
	case "status", "agents", "refresh", "rebase", "sync",
		"delegate", "escalate", "codex", "claude", "gemini", "ux", "test", "patch",
		"search", "web", "internet", "research", "read", "reflect", "deep", "work", "start",
		"memory", "memories", "remember", "learn", "forget", "unlearn":
		return c.message(strings.Join(args, " "))
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func (c cli) assistant(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: homelabctl assistant <list|show|archive|restore|signals|signal>")
	}
	action := commandWord(args[0])
	switch action {
	case "list", "ls", "runs", "decisions":
		if len(args) > 2 {
			return fmt.Errorf("usage: homelabctl assistant list [--all|--archived]")
		}
		endpoint := "/assistant/runs"
		if len(args) == 2 {
			switch commandWord(args[1]) {
			case "--all", "all":
				endpoint += "?archived=include"
			case "--archived", "archived":
				endpoint += "?archived=only"
			default:
				return fmt.Errorf("usage: homelabctl assistant list [--all|--archived]")
			}
		}
		return c.do(http.MethodGet, endpoint, nil)
	case "show", "get":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl assistant show <run_id>")
		}
		return c.do(http.MethodGet, path("assistant", "runs", args[1]), nil)
	case "archive":
		if len(args) < 2 {
			return fmt.Errorf("usage: homelabctl assistant archive <run_id> [reason]")
		}
		body := map[string]any{
			"archived": true,
			"actor":    "homelabctl",
			"reason":   strings.TrimSpace(strings.Join(args[2:], " ")),
		}
		return c.do(http.MethodPatch, path("assistant", "runs", args[1]), body)
	case "restore", "unarchive":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl assistant restore <run_id>")
		}
		return c.do(http.MethodPatch, path("assistant", "runs", args[1]), map[string]any{
			"archived": false,
			"actor":    "homelabctl",
		})
	case "signals", "inbox":
		if len(args) != 1 {
			return fmt.Errorf("usage: homelabctl assistant signals")
		}
		return c.do(http.MethodGet, "/assistant/signals", nil)
	case "signal":
		if len(args) < 3 {
			return fmt.Errorf("usage: homelabctl assistant signal <fingerprint> <useful|dismiss|snooze|create-task> [reason]")
		}
		feedback := commandWord(args[2])
		switch feedback {
		case "useful":
		case "dismiss", "dismissed":
			feedback = "dismiss"
		case "snooze", "snoozed":
			feedback = "snooze"
		case "create-task", "create_task", "follow-up", "followup":
			feedback = "create_task"
		default:
			return fmt.Errorf("usage: homelabctl assistant signal <fingerprint> <useful|dismiss|snooze|create-task> [reason]")
		}
		body := map[string]any{"feedback": feedback}
		if reason := strings.TrimSpace(strings.Join(args[3:], " ")); reason != "" {
			body["reason"] = reason
		}
		return c.do(http.MethodPatch, path("assistant", "signals", args[1]), body)
	default:
		return fmt.Errorf("unknown assistant command %q", args[0])
	}
}

func (c cli) goal(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: homelabctl goal <create|list|show|check|autopilot|pause|archive|note|watch>")
	}
	action := commandWord(args[0])
	switch action {
	case "list", "ls":
		if len(args) != 1 {
			return fmt.Errorf("usage: homelabctl goal list")
		}
		return c.do(http.MethodGet, "/assistant/goals", nil)
	case "show", "get":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl goal show <goal_id>")
		}
		return c.do(http.MethodGet, path("assistant", "goals", args[1]), nil)
	case "check":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl goal check <goal_id>")
		}
		return c.do(http.MethodPost, path("assistant", "goals", args[1], "check"), nil)
	case "autopilot", "auto":
		return c.goalAutopilot(args[1:])
	case "pause":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl goal pause <goal_id>")
		}
		return c.do(http.MethodPatch, path("assistant", "goals", args[1]), map[string]any{"status": "paused"})
	case "archive":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl goal archive <goal_id>")
		}
		return c.do(http.MethodPatch, path("assistant", "goals", args[1]), map[string]any{"status": "archived"})
	case "note":
		if len(args) < 3 {
			return fmt.Errorf("usage: homelabctl goal note <goal_id> <body>")
		}
		return c.do(http.MethodPost, path("assistant", "goals", args[1], "notes"), map[string]any{
			"body":       strings.TrimSpace(strings.Join(args[2:], " ")),
			"created_by": "homelabctl",
		})
	case "watch":
		if len(args) < 3 {
			return fmt.Errorf("usage: homelabctl goal watch <goal_id> <title>")
		}
		return c.do(http.MethodPost, path("assistant", "goals", args[1], "watches"), map[string]any{
			"title":      strings.TrimSpace(strings.Join(args[2:], " ")),
			"source":     "homelabctl",
			"on_trigger": "create_signal",
		})
	case "create", "new", "add":
		return c.goalCreate(args[1:])
	default:
		return c.goalCreate(args)
	}
}

func (c cli) goalCreate(args []string) error {
	flags := flag.NewFlagSet("goal create", flag.ContinueOnError)
	flags.SetOutput(c.err)
	title := flags.String("title", "", "goal title")
	details := flags.String("details", "", "long-form goal details")
	kind := flags.String("kind", "", "goal kind: build, routine, watch, or maintenance")
	mode := flags.String("mode", "", "goal execution mode: guided or autopilot")
	targetMode := flags.String("target", "", "goal task target: auto, local, or remote")
	projectID := flags.String("project", "", "remote project/workspace id for Goal tasks")
	agentID := flags.String("agent", "", "remote agent id for Goal tasks")
	workdirID := flags.String("workdir", "", "remote workdir id for Goal tasks")
	workdirPath := flags.String("workdir-path", "", "remote workdir path for Goal tasks")
	priority := flags.String("priority", "", "goal priority")
	autonomy := flags.String("autonomy", "", "goal autonomy")
	cadence := flags.String("cadence", "", "goal cadence, such as daily, hourly, or 4h")
	budgetTasks := flags.Int("budget-tasks", 0, "autopilot task budget")
	var successCriteria stringListFlag
	var constraints stringListFlag
	flags.Var(&successCriteria, "success", "success criterion; repeat for multiple criteria")
	flags.Var(&constraints, "constraint", "constraint; repeat for multiple constraints")
	if err := flags.Parse(args); err != nil {
		return err
	}
	objective := strings.TrimSpace(strings.Join(flags.Args(), " "))
	if objective == "" {
		return fmt.Errorf("usage: homelabctl goal create [--title TITLE] [--target auto|local|remote] [--project PROJECT] [--cadence daily] [--success TEXT] [--constraint TEXT] <objective>")
	}
	body := map[string]any{
		"objective":  objective,
		"created_by": "homelabctl",
	}
	if value := strings.TrimSpace(*title); value != "" {
		body["title"] = value
	} else {
		body["title"] = objective
	}
	if value := strings.TrimSpace(*details); value != "" {
		body["details"] = value
	}
	if value := strings.TrimSpace(*kind); value != "" {
		body["kind"] = value
	}
	if value := strings.TrimSpace(*mode); value != "" {
		body["execution_mode"] = value
	}
	if target := taskTargetFromFlags(*targetMode, *projectID, *agentID, *workdirID, *workdirPath, ""); len(target) > 0 {
		body["target"] = target
	}
	if *budgetTasks > 0 {
		body["autopilot"] = map[string]any{"budget_tasks": *budgetTasks}
	}
	if value := strings.TrimSpace(*priority); value != "" {
		body["priority"] = value
	}
	if value := strings.TrimSpace(*autonomy); value != "" {
		body["autonomy"] = value
	}
	if value := strings.TrimSpace(*cadence); value != "" {
		body["cadence"] = value
	}
	if len(successCriteria) > 0 {
		body["success_criteria"] = []string(successCriteria)
	}
	if len(constraints) > 0 {
		body["constraints"] = []string(constraints)
	}
	return c.do(http.MethodPost, "/assistant/goals", body)
}

func (c cli) goalAutopilot(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: homelabctl goal autopilot <start|pause|resume|stop> [--budget-tasks N] [--max-minutes N] <goal_id>")
	}
	action := commandWord(args[0])
	if action != "start" && action != "pause" && action != "resume" && action != "stop" {
		return fmt.Errorf("usage: homelabctl goal autopilot <start|pause|resume|stop> [--budget-tasks N] [--max-minutes N] <goal_id>")
	}
	flags := flag.NewFlagSet("goal autopilot "+action, flag.ContinueOnError)
	flags.SetOutput(c.err)
	budgetTasks := flags.Int("budget-tasks", 0, "autopilot task budget")
	maxMinutes := flags.Int("max-minutes", 0, "autopilot runtime budget in minutes")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: homelabctl goal autopilot %s [--budget-tasks N] [--max-minutes N] <goal_id>", action)
	}
	body := map[string]any{}
	if *budgetTasks > 0 {
		body["budget_tasks"] = *budgetTasks
	}
	if *maxMinutes > 0 {
		body["max_runtime_minutes"] = *maxMinutes
	}
	return c.do(http.MethodPost, path("assistant", "goals", flags.Arg(0), "autopilot", action), body)
}

func (c cli) chat(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: homelabctl chat <message|clear>")
	}
	switch commandWord(args[0]) {
	case "clear":
		all := false
		conversationID := ""
		for _, arg := range args[1:] {
			switch commandWord(arg) {
			case "--all", "all":
				all = true
			default:
				if strings.TrimSpace(arg) != "" {
					conversationID = strings.TrimSpace(arg)
				}
			}
		}
		if !all && conversationID == "" {
			return fmt.Errorf("usage: homelabctl chat clear <conversation_id>|--all")
		}
		body := map[string]any{"all": all}
		if conversationID != "" {
			body["conversation_id"] = conversationID
		}
		return c.do(http.MethodPost, "/chat/clear", body)
	default:
		return c.message(strings.Join(args, " "))
	}
}

func (c cli) workflow(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: homelabctl workflow <new|list|show|run>")
	}
	action := commandWord(args[0])
	switch action {
	case "new", "create":
		name, goal := parseWorkflowCreateArgs(args[1:])
		if name == "" {
			return fmt.Errorf("usage: homelabctl workflow new <name>: <goal>")
		}
		return c.do(http.MethodPost, "/workflows", map[string]any{"name": name, "goal": goal})
	case "list", "ls":
		if len(args) != 1 {
			return fmt.Errorf("usage: homelabctl workflow list")
		}
		return c.do(http.MethodGet, "/workflows", nil)
	case "show", "get":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl workflow show <workflow_id>")
		}
		return c.do(http.MethodGet, path("workflows", args[1]), nil)
	case "run", "start":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl workflow run <workflow_id>")
		}
		return c.do(http.MethodPost, path("workflows", args[1], "run"), nil)
	default:
		return fmt.Errorf("unknown workflow command %q", args[0])
	}
}

func parseWorkflowCreateArgs(args []string) (string, string) {
	text := strings.TrimSpace(strings.Join(args, " "))
	if text == "" {
		return "", ""
	}
	if name, goal, ok := strings.Cut(text, ":"); ok {
		name = strings.TrimSpace(name)
		goal = strings.TrimSpace(goal)
		if goal == "" {
			goal = name
		}
		return name, goal
	}
	return text, text
}

func (c cli) settings(args []string) error {
	if len(args) == 0 {
		return c.do(http.MethodGet, "/settings", nil)
	}
	action := commandWord(args[0])
	switch action {
	case "show", "get", "list":
		if len(args) != 1 {
			return fmt.Errorf("usage: homelabctl settings")
		}
		return c.do(http.MethodGet, "/settings", nil)
	case "auto-merge", "automerge":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl settings auto-merge <on|off>")
		}
		enabled, err := parseOnOff(args[1])
		if err != nil {
			return err
		}
		return c.do(http.MethodPost, "/settings", map[string]any{"auto_merge_enabled": enabled})
	default:
		return fmt.Errorf("unknown settings command %q", args[0])
	}
}

func parseOnOff(value string) (bool, error) {
	switch commandWord(value) {
	case "on", "true", "yes", "enable", "enabled", "1":
		return true, nil
	case "off", "false", "no", "disable", "disabled", "0":
		return false, nil
	default:
		return false, fmt.Errorf("expected on or off, got %q", value)
	}
}

func (c cli) knowledge(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: homelabctl knowledge <list|show|create|update|delete|source|query|ask|research|research-run>")
	}
	action := commandWord(args[0])
	switch action {
	case "list", "ls":
		if len(args) != 1 {
			return fmt.Errorf("usage: homelabctl knowledge list")
		}
		return c.do(http.MethodGet, "/knowledge/spaces", nil)
	case "show", "get":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl knowledge show <space_id>")
		}
		return c.do(http.MethodGet, path("knowledge", "spaces", args[1]), nil)
	case "create", "new":
		return c.knowledgeCreate(args[1:])
	case "update", "edit", "rename":
		return c.knowledgeUpdate(args[1:])
	case "delete", "remove", "rm":
		return c.knowledgeDelete(args[1:])
	case "source", "sources":
		return c.knowledgeSource(args[1:])
	case "add-source", "add":
		return c.knowledgeSource(withAction("add", args[1:]))
	case "research", "report":
		return c.knowledgeResearch(args[1:])
	case "query", "search":
		return c.knowledgeQuery(args[1:])
	case "ask", "answer":
		return c.knowledgeAsk(args[1:])
	case "research-run", "run":
		return c.knowledgeResearchRun(args[1:])
	default:
		return fmt.Errorf("unknown knowledge command %q", args[0])
	}
}

func (c cli) knowledgeCreate(args []string) error {
	flags := flag.NewFlagSet("knowledge create", flag.ContinueOnError)
	flags.SetOutput(c.err)
	objective := flags.String("objective", "", "space objective")
	description := flags.String("description", "", "space description")
	createdBy := flags.String("created-by", "", "creator label")
	if err := flags.Parse(args); err != nil {
		return err
	}
	title := strings.TrimSpace(strings.Join(flags.Args(), " "))
	if title == "" {
		return fmt.Errorf("usage: homelabctl knowledge create [--objective TEXT] [--description TEXT] [--created-by NAME] <title>")
	}
	body := map[string]any{"title": title}
	if value := strings.TrimSpace(*objective); value != "" {
		body["objective"] = value
	}
	if value := strings.TrimSpace(*description); value != "" {
		body["description"] = value
	}
	if value := strings.TrimSpace(*createdBy); value != "" {
		body["created_by"] = value
	}
	return c.do(http.MethodPost, "/knowledge/spaces", body)
}

func (c cli) knowledgeUpdate(args []string) error {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return fmt.Errorf("usage: homelabctl knowledge update <space_id> [--title TITLE] [--objective TEXT] [--description TEXT]")
	}
	spaceID := args[0]
	flags := flag.NewFlagSet("knowledge update", flag.ContinueOnError)
	flags.SetOutput(c.err)
	title := flags.String("title", "", "space title")
	objective := flags.String("objective", "", "space objective")
	description := flags.String("description", "", "space description")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if len(flags.Args()) > 0 && strings.TrimSpace(*title) == "" {
		*title = strings.TrimSpace(strings.Join(flags.Args(), " "))
	}
	body := map[string]any{}
	if value := strings.TrimSpace(*title); value != "" {
		body["title"] = value
	}
	if value := strings.TrimSpace(*objective); value != "" {
		body["objective"] = value
	}
	if value := strings.TrimSpace(*description); value != "" {
		body["description"] = value
	}
	if len(body) == 0 {
		return fmt.Errorf("usage: homelabctl knowledge update <space_id> [--title TITLE] [--objective TEXT] [--description TEXT]")
	}
	return c.do(http.MethodPatch, path("knowledge", "spaces", spaceID), body)
}

func (c cli) knowledgeDelete(args []string) error {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		return fmt.Errorf("usage: homelabctl knowledge delete <space_id>")
	}
	return c.do(http.MethodDelete, path("knowledge", "spaces", args[0]), nil)
}

func (c cli) knowledgeSource(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: homelabctl knowledge source <add|delete>")
	}
	switch commandWord(args[0]) {
	case "add", "create":
		return c.knowledgeSourceAdd(args[1:])
	case "delete", "remove", "rm":
		return c.knowledgeSourceDelete(args[1:])
	default:
		return fmt.Errorf("unknown knowledge source command %q", args[0])
	}
}

func (c cli) knowledgeSourceAdd(args []string) error {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return fmt.Errorf("usage: homelabctl knowledge source add <space_id> [--kind text|url|file|note|email|mcp] [--url URL|--uri URI] [--file PATH|--content TEXT] [title]")
	}
	spaceID := args[0]
	flags := flag.NewFlagSet("knowledge source add", flag.ContinueOnError)
	flags.SetOutput(c.err)
	kind := flags.String("kind", "", "source kind: text, url, file, note, email, or mcp")
	uri := flags.String("uri", "", "source reference URI or path")
	urlValue := flags.String("url", "", "fetch and add a web URL")
	filePath := flags.String("file", "", "read source content from this file, or - for stdin")
	contentFlag := flags.String("content", "", "source content")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	title := strings.TrimSpace(strings.Join(flags.Args(), " "))
	fileValue := strings.TrimSpace(*filePath)
	contentValue := *contentFlag
	if fileValue != "" && strings.TrimSpace(contentValue) != "" {
		return fmt.Errorf("--file and --content cannot be used together")
	}
	sourceURI := strings.TrimSpace(*uri)
	if value := strings.TrimSpace(*urlValue); value != "" {
		sourceURI = value
	}
	if fileValue != "" {
		content, err := readKnowledgeSourceContent(c.in, fileValue)
		if err != nil {
			return err
		}
		contentValue = content
	}
	sourceKind := strings.TrimSpace(*kind)
	if sourceKind == "" && strings.TrimSpace(*urlValue) != "" {
		sourceKind = "url"
	}
	if sourceKind == "" && fileValue != "" {
		sourceKind = "file"
	}
	if title == "" && sourceKind == "url" && sourceURI != "" {
		title = sourceURI
	}
	if title == "" {
		return fmt.Errorf("usage: homelabctl knowledge source add <space_id> [--kind text|url|file|note|email|mcp] [--url URL|--uri URI] [--file PATH|--content TEXT] [title]")
	}
	if strings.TrimSpace(contentValue) == "" && !(sourceKind == "url" && sourceURI != "") {
		return fmt.Errorf("knowledge source content is required; use --file, --content, or --url for fetchable URLs")
	}
	body := map[string]any{"title": title}
	if strings.TrimSpace(contentValue) != "" {
		body["content"] = contentValue
	}
	if sourceKind != "" {
		body["kind"] = sourceKind
	}
	if sourceURI == "" && fileValue != "" && fileValue != "-" {
		sourceURI = fileValue
	}
	if sourceURI != "" {
		body["uri"] = sourceURI
	}
	return c.do(http.MethodPost, path("knowledge", "spaces", spaceID, "sources"), body)
}

func (c cli) knowledgeSourceDelete(args []string) error {
	if len(args) != 2 || strings.TrimSpace(args[0]) == "" || strings.TrimSpace(args[1]) == "" {
		return fmt.Errorf("usage: homelabctl knowledge source delete <space_id> <source_id>")
	}
	return c.do(http.MethodDelete, path("knowledge", "spaces", args[0], "sources", args[1]), nil)
}

func readKnowledgeSourceContent(in io.Reader, filePath string) (string, error) {
	if filePath == "-" {
		data, err := io.ReadAll(in)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (c cli) knowledgeResearch(args []string) error {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return fmt.Errorf("usage: homelabctl knowledge research <space_id> [--mode research|brief|study] [--source SOURCE_ID]... <question>")
	}
	spaceID := args[0]
	flags := flag.NewFlagSet("knowledge research", flag.ContinueOnError)
	flags.SetOutput(c.err)
	mode := flags.String("mode", "", "research mode: research, brief, or study")
	var sourceIDs stringListFlag
	flags.Var(&sourceIDs, "source", "limit research to a source id; repeat for multiple sources")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	question := strings.TrimSpace(strings.Join(flags.Args(), " "))
	if question == "" {
		return fmt.Errorf("usage: homelabctl knowledge research <space_id> [--mode research|brief|study] [--source SOURCE_ID]... <question>")
	}
	body := map[string]any{"question": question}
	if value := strings.TrimSpace(*mode); value != "" {
		body["mode"] = value
	}
	if len(sourceIDs) > 0 {
		body["source_ids"] = []string(sourceIDs)
	}
	return c.do(http.MethodPost, path("knowledge", "spaces", spaceID, "research"), body)
}

func (c cli) knowledgeQuery(args []string) error {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return fmt.Errorf("usage: homelabctl knowledge query <space_id> [--limit N] [--source SOURCE_ID]... <query>")
	}
	spaceID := args[0]
	flags := flag.NewFlagSet("knowledge query", flag.ContinueOnError)
	flags.SetOutput(c.err)
	limit := flags.Int("limit", 0, "maximum evidence chunks to return")
	var sourceIDs stringListFlag
	flags.Var(&sourceIDs, "source", "limit query to a source id; repeat for multiple sources")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	query := strings.TrimSpace(strings.Join(flags.Args(), " "))
	if query == "" {
		return fmt.Errorf("usage: homelabctl knowledge query <space_id> [--limit N] [--source SOURCE_ID]... <query>")
	}
	body := map[string]any{"query": query}
	if *limit > 0 {
		body["limit"] = *limit
	}
	if len(sourceIDs) > 0 {
		body["source_ids"] = []string(sourceIDs)
	}
	return c.do(http.MethodPost, path("knowledge", "spaces", spaceID, "query"), body)
}

func (c cli) knowledgeAsk(args []string) error {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return fmt.Errorf("usage: homelabctl knowledge ask <space_id> [--limit N] [--source SOURCE_ID]... <question>")
	}
	spaceID := args[0]
	flags := flag.NewFlagSet("knowledge ask", flag.ContinueOnError)
	flags.SetOutput(c.err)
	limit := flags.Int("limit", 0, "maximum evidence chunks to use")
	var sourceIDs stringListFlag
	flags.Var(&sourceIDs, "source", "limit answer to a source id; repeat for multiple sources")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	question := strings.TrimSpace(strings.Join(flags.Args(), " "))
	if question == "" {
		return fmt.Errorf("usage: homelabctl knowledge ask <space_id> [--limit N] [--source SOURCE_ID]... <question>")
	}
	body := map[string]any{"question": question}
	if *limit > 0 {
		body["limit"] = *limit
	}
	if len(sourceIDs) > 0 {
		body["source_ids"] = []string(sourceIDs)
	}
	return c.do(http.MethodPost, path("knowledge", "spaces", spaceID, "ask"), body)
}

func (c cli) knowledgeResearchRun(args []string) error {
	if len(args) > 0 {
		switch commandWord(args[0]) {
		case "resume", "retry":
			return c.knowledgeResearchRunResume(args[1:])
		}
	}
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return fmt.Errorf("usage: homelabctl knowledge research-run <space_id> [--depth quick|standard|deep] [--scope TEXT] [--mode research|brief|study] [--discover] [--source SOURCE_ID]... <objective>")
	}
	spaceID := args[0]
	flags := flag.NewFlagSet("knowledge research-run", flag.ContinueOnError)
	flags.SetOutput(c.err)
	depth := flags.String("depth", "", "research depth: quick, standard, or deep")
	scope := flags.String("scope", "", "research scope")
	mode := flags.String("mode", "", "report mode: research, brief, or study")
	discover := flags.Bool("discover", false, "search online and import fetched sources before synthesis")
	var sourceIDs stringListFlag
	flags.Var(&sourceIDs, "source", "limit run to a source id; repeat for multiple sources")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	objective := strings.TrimSpace(strings.Join(flags.Args(), " "))
	if objective == "" {
		return fmt.Errorf("usage: homelabctl knowledge research-run <space_id> [--depth quick|standard|deep] [--scope TEXT] [--mode research|brief|study] [--discover] [--source SOURCE_ID]... <objective>")
	}
	body := map[string]any{"objective": objective}
	if value := strings.TrimSpace(*depth); value != "" {
		body["depth"] = value
	}
	if value := strings.TrimSpace(*scope); value != "" {
		body["scope"] = value
	}
	if value := strings.TrimSpace(*mode); value != "" {
		body["mode"] = value
	}
	if len(sourceIDs) > 0 {
		body["source_ids"] = []string(sourceIDs)
	}
	if *discover {
		body["discover_sources"] = true
	}
	return c.do(http.MethodPost, path("knowledge", "spaces", spaceID, "research-runs"), body)
}

func (c cli) knowledgeResearchRunResume(args []string) error {
	if len(args) != 2 || strings.TrimSpace(args[0]) == "" || strings.TrimSpace(args[1]) == "" {
		return fmt.Errorf("usage: homelabctl knowledge research-run resume <space_id> <run_id>")
	}
	return c.do(http.MethodPost, path("knowledge", "spaces", args[0], "research-runs", args[1], "resume"), nil)
}

func (c cli) task(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: homelabctl task <new|list|show|runs|diff|run|review|review-ui|queue|accept|restart|reopen|cancel|retry|delete>")
	}
	action := commandWord(args[0])
	switch action {
	case "new", "create":
		target, attachments, rest, err := parseTaskNewArgs(args[1:])
		if err != nil {
			return err
		}
		goal := strings.TrimSpace(strings.Join(rest, " "))
		if goal == "" {
			return fmt.Errorf("usage: homelabctl task new [--attach <path>] [--auto|--local|--project <project>|--agent <agent_id> --workdir <path_or_id>] <goal>")
		}
		body := map[string]any{"goal": goal}
		if target != nil {
			body["target"] = target
		}
		if len(attachments) > 0 {
			body["attachments"] = attachments
		}
		return c.do(http.MethodPost, "/tasks", body)
	case "list", "ls":
		if len(args) != 1 {
			return fmt.Errorf("usage: homelabctl task list")
		}
		return c.do(http.MethodGet, "/tasks", nil)
	case "show", "get":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl task show <task_id>")
		}
		return c.do(http.MethodGet, path("tasks", args[1]), nil)
	case "runs":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl task runs <task_id>")
		}
		return c.do(http.MethodGet, path("tasks", args[1], "runs"), nil)
	case "diff":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl task diff <task_id>")
		}
		return c.printTaskDiff(path("tasks", args[1], "diff"))
	case "run", "review", "review-ui":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl task %s <task_id>", action)
		}
		return c.do(http.MethodPost, path("tasks", args[1], action), nil)
	case "queue", "merge-queue":
		if len(args) != 3 {
			return fmt.Errorf("usage: homelabctl task queue <task_id> <up|down>")
		}
		return c.do(http.MethodPost, path("tasks", args[1], "merge-queue"), map[string]any{"direction": args[2]})
	case "accept", "verify":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl task accept <task_id>")
		}
		return c.do(http.MethodPost, path("tasks", args[1], "accept"), nil)
	case "restart":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl task restart <task_id>")
		}
		return c.do(http.MethodPost, path("tasks", args[1], "restart"), nil)
	case "cancel", "stop":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl task cancel <task_id>")
		}
		return c.do(http.MethodPost, path("tasks", args[1], "cancel"), nil)
	case "delete", "remove", "rm":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl task delete <task_id>")
		}
		return c.do(http.MethodPost, path("tasks", args[1], "delete"), nil)
	case "reopen":
		if len(args) < 2 {
			return fmt.Errorf("usage: homelabctl task reopen <task_id> [reason]")
		}
		return c.do(http.MethodPost, path("tasks", args[1], "reopen"), map[string]any{"reason": strings.Join(args[2:], " ")})
	case "retry":
		if len(args) < 2 {
			return fmt.Errorf("usage: homelabctl task retry <task_id> [backend] [instruction]")
		}
		backend, instruction := retryArgs(args[2:])
		return c.do(http.MethodPost, path("tasks", args[1], "retry"), map[string]any{"backend": backend, "instruction": instruction})
	default:
		return fmt.Errorf("unknown task command %q", args[0])
	}
}

func parseTaskNewArgs(args []string) (map[string]any, []map[string]any, []string, error) {
	target := map[string]any{}
	usedTarget := false
	var attachments []map[string]any
	var rest []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--local":
			target["mode"] = "local"
			usedTarget = true
			continue
		case "--auto":
			target["mode"] = "auto"
			usedTarget = true
			continue
		case "--project":
			if i+1 < len(args) {
				target["project_id"] = args[i+1]
				usedTarget = true
				i++
				continue
			}
		case "--agent":
			if i+1 < len(args) {
				target["agent_id"] = args[i+1]
				usedTarget = true
				i++
				continue
			}
		case "--workdir":
			if i+1 < len(args) {
				target["workdir_id"] = args[i+1]
				usedTarget = true
				i++
				continue
			}
		case "--workdir-path":
			if i+1 < len(args) {
				target["workdir"] = args[i+1]
				usedTarget = true
				i++
				continue
			}
		case "--backend":
			if i+1 < len(args) {
				target["backend"] = args[i+1]
				usedTarget = true
				i++
				continue
			}
		case "--attach":
			if i+1 >= len(args) {
				return nil, nil, nil, fmt.Errorf("--attach requires a path")
			}
			attachment, err := fileAttachment(args[i+1])
			if err != nil {
				return nil, nil, nil, err
			}
			attachments = append(attachments, attachment)
			i++
			continue
		}
		rest = append(rest, args[i])
	}
	if !usedTarget {
		target = nil
	} else if _, ok := target["mode"]; !ok {
		if _, hasAgent := target["agent_id"]; hasAgent {
			target["mode"] = "remote"
		} else if _, hasWorkdirID := target["workdir_id"]; hasWorkdirID {
			target["mode"] = "remote"
		} else if _, hasWorkdir := target["workdir"]; hasWorkdir {
			target["mode"] = "remote"
		} else if _, hasBackend := target["backend"]; hasBackend {
			target["mode"] = "remote"
		} else {
			target["mode"] = "auto"
		}
	}
	return target, attachments, rest, nil
}

func taskTargetFromFlags(mode, projectID, agentID, workdirID, workdirPath, backend string) map[string]any {
	target := map[string]any{}
	if value := strings.TrimSpace(mode); value != "" {
		target["mode"] = value
	}
	if value := strings.TrimSpace(projectID); value != "" {
		target["project_id"] = value
	}
	if value := strings.TrimSpace(agentID); value != "" {
		target["agent_id"] = value
	}
	if value := strings.TrimSpace(workdirID); value != "" {
		target["workdir_id"] = value
	}
	if value := strings.TrimSpace(workdirPath); value != "" {
		target["workdir"] = value
	}
	if value := strings.TrimSpace(backend); value != "" {
		target["backend"] = value
	}
	if len(target) == 0 {
		return nil
	}
	if _, ok := target["mode"]; !ok {
		target["mode"] = "auto"
	}
	return target
}

func fileAttachment(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	attachment := map[string]any{
		"name":         filepath.Base(path),
		"content_type": contentType,
		"size":         len(data),
		"data_url":     "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data),
	}
	if isTextAttachment(contentType) {
		text := string(data)
		if len(text) > 128*1024 {
			text = text[:128*1024] + "\n\n[truncated]"
		}
		attachment["text"] = text
	}
	return attachment, nil
}

func isTextAttachment(contentType string) bool {
	contentType = strings.ToLower(contentType)
	return strings.HasPrefix(contentType, "text/") ||
		strings.Contains(contentType, "json") ||
		strings.Contains(contentType, "yaml") ||
		strings.Contains(contentType, "xml")
}

func (c cli) agent(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: homelabctl agent <list|show>")
	}
	switch commandWord(args[0]) {
	case "list", "ls":
		if len(args) != 1 {
			return fmt.Errorf("usage: homelabctl agent list")
		}
		return c.do(http.MethodGet, "/agents", nil)
	case "show":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl agent show <agent_id>")
		}
		return c.do(http.MethodGet, path("agents", args[1]), nil)
	default:
		return fmt.Errorf("unknown agent command %q", args[0])
	}
}

func (c cli) workspace(args []string) error {
	if len(args) == 0 {
		args = []string{"list"}
	}
	switch commandWord(args[0]) {
	case "list", "ls":
		if len(args) != 1 {
			return fmt.Errorf("usage: homelabctl workspace list")
		}
		return c.do(http.MethodGet, "/workspaces", nil)
	default:
		return fmt.Errorf("unknown workspace command %q", args[0])
	}
}

func (c cli) approval(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: homelabctl approval <list|approve|deny|edit>")
	}
	action := commandWord(args[0])
	switch action {
	case "list", "ls":
		if len(args) != 1 {
			return fmt.Errorf("usage: homelabctl approval list")
		}
		return c.do(http.MethodGet, "/approvals", nil)
	case "approve", "deny":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl approval %s <approval_id>", action)
		}
		return c.do(http.MethodPost, path("approvals", args[1], action), nil)
	case "edit":
		if len(args) < 3 {
			return fmt.Errorf("usage: homelabctl approval edit <approval_id> <json_args>")
		}
		rawArgs := strings.TrimSpace(strings.Join(args[2:], " "))
		if !json.Valid([]byte(rawArgs)) {
			return fmt.Errorf("approval args must be valid JSON")
		}
		return c.do(http.MethodPost, path("approvals", args[1], "edit"), struct {
			Args json.RawMessage `json:"args"`
		}{Args: json.RawMessage(rawArgs)})
	default:
		return fmt.Errorf("unknown approval command %q", args[0])
	}
}

func (c cli) events(args []string) error {
	flags := flag.NewFlagSet("events", flag.ContinueOnError)
	flags.SetOutput(c.err)
	limit := flags.Int("limit", 0, "maximum number of recent events to return")
	if err := flags.Parse(args); err != nil {
		return err
	}
	rest := flags.Args()
	if len(rest) > 1 {
		return fmt.Errorf("usage: homelabctl events [-limit N] [YYYY-MM-DD]")
	}
	query := url.Values{}
	if len(rest) == 1 {
		query.Set("date", rest[0])
	}
	if *limit < 0 {
		return fmt.Errorf("limit must be a positive integer")
	}
	if *limit > 0 {
		query.Set("limit", strconv.Itoa(*limit))
	}
	endpoint := "/events"
	if encoded := query.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	return c.do(http.MethodGet, endpoint, nil)
}

func (c cli) healthd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: homelabctl healthd <errors>")
	}
	switch commandWord(args[0]) {
	case "errors", "error":
		flags := flag.NewFlagSet("healthd errors", flag.ContinueOnError)
		flags.SetOutput(c.err)
		limit := flags.Int("limit", 50, "maximum number of recent errors to return")
		source := flags.String("source", "", "filter errors by source")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		rest := flags.Args()
		if len(rest) > 1 {
			return fmt.Errorf("usage: homelabctl healthd errors [-limit N] [-source SOURCE] [app]")
		}
		if *limit <= 0 {
			return fmt.Errorf("limit must be a positive integer")
		}
		query := url.Values{}
		query.Set("limit", strconv.Itoa(*limit))
		if strings.TrimSpace(*source) != "" {
			query.Set("source", strings.TrimSpace(*source))
		}
		if len(rest) == 1 {
			query.Set("app", rest[0])
		}
		endpoint := "/healthd/errors"
		if encoded := query.Encode(); encoded != "" {
			endpoint += "?" + encoded
		}
		return c.doAt(c.healthdBase, http.MethodGet, endpoint, nil)
	default:
		return fmt.Errorf("unknown healthd command %q", args[0])
	}
}

func (c cli) supervisor(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: homelabctl supervisor <status|apps|start|stop|restart|app>")
	}
	action := commandWord(args[0])
	switch action {
	case "status", "show", "snapshot":
		if len(args) != 1 {
			return fmt.Errorf("usage: homelabctl supervisor status")
		}
		return c.doAt(c.supervisordBase, http.MethodGet, "/supervisord", nil)
	case "apps", "list", "ls":
		if len(args) != 1 {
			return fmt.Errorf("usage: homelabctl supervisor apps")
		}
		return c.doAt(c.supervisordBase, http.MethodGet, "/supervisord/apps", nil)
	case "start":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl supervisor start <app>")
		}
		return c.supervisorAppAction("start", args[1], 0)
	case "stop", "restart":
		switch len(args) {
		case 1:
			return c.doAt(c.supervisordBase, http.MethodPost, path("supervisord", action), nil)
		case 2:
			return c.supervisorAppAction(action, args[1], 0)
		default:
			return fmt.Errorf("usage: homelabctl supervisor %s [app]", action)
		}
	case "app":
		return c.supervisorApp(args[1:])
	default:
		return fmt.Errorf("unknown supervisor command %q", args[0])
	}
}

func (c cli) supervisorApp(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: homelabctl supervisor app <start|stop|restart|adopt> <app> [pid]")
	}
	action := commandWord(args[0])
	switch action {
	case "start", "stop", "restart":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl supervisor app %s <app>", action)
		}
		return c.supervisorAppAction(action, args[1], 0)
	case "adopt":
		if len(args) != 3 {
			return fmt.Errorf("usage: homelabctl supervisor app adopt <app> <pid>")
		}
		pid, err := strconv.Atoi(args[2])
		if err != nil || pid <= 0 {
			return fmt.Errorf("pid must be a positive integer")
		}
		return c.supervisorAppAction(action, args[1], pid)
	default:
		return fmt.Errorf("unknown supervisor app command %q", args[0])
	}
}

func (c cli) supervisorAppAction(action, app string, pid int) error {
	var body any
	if action == "adopt" {
		body = map[string]any{"pid": pid}
	}
	return c.doAt(c.supervisordBase, http.MethodPost, path("supervisord", "apps", app, action), body)
}

func (c cli) terminal(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: homelabctl terminal <start|show|send|input|stream|signal|close>")
	}
	action := commandWord(args[0])
	switch action {
	case "start", "new", "create":
		if len(args) > 2 {
			return fmt.Errorf("usage: homelabctl terminal start [cwd]")
		}
		body := map[string]any{}
		if len(args) == 2 {
			body["cwd"] = args[1]
		}
		return c.do(http.MethodPost, "/terminal/sessions", body)
	case "show", "get":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl terminal show <session_id>")
		}
		return c.do(http.MethodGet, path("terminal", "sessions", args[1]), nil)
	case "send":
		if len(args) < 3 {
			return fmt.Errorf("usage: homelabctl terminal send <session_id> <text>")
		}
		data := strings.Join(args[2:], " ")
		if !strings.HasSuffix(data, "\n") {
			data += "\n"
		}
		return c.do(http.MethodPost, path("terminal", "sessions", args[1], "input"), map[string]any{"data": data})
	case "input":
		if len(args) < 3 {
			return fmt.Errorf("usage: homelabctl terminal input <session_id> <text>")
		}
		return c.do(http.MethodPost, path("terminal", "sessions", args[1], "input"), map[string]any{"data": strings.Join(args[2:], " ")})
	case "stream", "events", "attach":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl terminal stream <session_id>")
		}
		return c.streamTerminal(args[1])
	case "signal":
		if len(args) != 3 {
			return fmt.Errorf("usage: homelabctl terminal signal <session_id> <interrupt|suspend|terminate>")
		}
		return c.do(http.MethodPost, path("terminal", "sessions", args[1], "signal"), map[string]any{"signal": commandWord(args[2])})
	case "interrupt", "suspend", "terminate":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl terminal %s <session_id>", action)
		}
		return c.do(http.MethodPost, path("terminal", "sessions", args[1], "signal"), map[string]any{"signal": action})
	case "close", "delete", "rm":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl terminal close <session_id>")
		}
		return c.do(http.MethodDelete, path("terminal", "sessions", args[1]), nil)
	default:
		return fmt.Errorf("unknown terminal command %q", args[0])
	}
}

func (c cli) message(message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return fmt.Errorf("usage: homelabctl message <text>")
	}
	out, err := c.request(http.MethodPost, "/message", map[string]any{"from": c.from, "content": message})
	if err != nil {
		return err
	}
	if c.json {
		return c.printResponse(out)
	}
	var reply struct {
		Reply   string   `json:"reply"`
		Buttons []string `json:"buttons"`
	}
	if err := json.Unmarshal(out, &reply); err == nil && strings.TrimSpace(reply.Reply) != "" {
		fmt.Fprintln(c.out, reply.Reply)
		for i, button := range reply.Buttons {
			label := strings.TrimSpace(button)
			if label == "" {
				continue
			}
			fmt.Fprintf(c.out, "%d. %s\n", i+1, label)
		}
		return nil
	}
	return c.printResponse(out)
}

func (c cli) printTaskDiff(endpoint string) error {
	out, err := c.request(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	if c.json {
		return c.printResponse(out)
	}
	var diff struct {
		BaseLabel string `json:"base_label"`
		RawDiff   string `json:"raw_diff"`
		Summary   struct {
			Files     int `json:"files"`
			Additions int `json:"additions"`
			Deletions int `json:"deletions"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(out, &diff); err != nil {
		return c.printResponse(out)
	}
	if strings.EqualFold(diff.BaseLabel, "remote agent") {
		fmt.Fprintln(c.out, "remote task diff is recorded by the remote agent")
		return nil
	}
	if strings.TrimSpace(diff.RawDiff) == "" {
		fmt.Fprintln(c.out, "no diff")
		return nil
	}
	fmt.Fprintf(c.out, "# %d changed file(s), +%d/-%d\n", diff.Summary.Files, diff.Summary.Additions, diff.Summary.Deletions)
	fmt.Fprint(c.out, diff.RawDiff)
	if !strings.HasSuffix(diff.RawDiff, "\n") {
		fmt.Fprintln(c.out)
	}
	return nil
}

func (c cli) shell() error {
	fmt.Fprintln(c.out, "homelabctl interactive shell. Type homelabd commands; type exit or quit to leave.")
	scanner := bufio.NewScanner(c.in)
	for {
		fmt.Fprint(c.out, "homelabctl> ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		switch commandWord(line) {
		case "exit", "quit":
			return nil
		}
		if err := c.message(line); err != nil {
			fmt.Fprintln(c.err, "homelabctl:", err)
		}
	}
	return scanner.Err()
}

func (c cli) streamTerminal(sessionID string) error {
	req, err := http.NewRequest(http.MethodGet, c.base+path("terminal", "sessions", sessionID, "events"), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		out, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return readErr
		}
		return fmt.Errorf("%s: %s", resp.Status, responseError(out))
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)
	var eventName string
	var dataLines []string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if done := c.printTerminalEvent(eventName, strings.Join(dataLines, "\n")); done {
				return nil
			}
			eventName = ""
			dataLines = nil
			continue
		}
		if value, ok := strings.CutPrefix(line, "event:"); ok {
			eventName = strings.TrimSpace(value)
			continue
		}
		if value, ok := strings.CutPrefix(line, "data:"); ok {
			dataLines = append(dataLines, strings.TrimSpace(value))
		}
	}
	if len(dataLines) > 0 {
		c.printTerminalEvent(eventName, strings.Join(dataLines, "\n"))
	}
	return scanner.Err()
}

func (c cli) printTerminalEvent(eventName, data string) bool {
	if eventName == "" || eventName == "ready" {
		return false
	}
	var event struct {
		Type string `json:"type"`
		Data string `json:"data"`
		Code int    `json:"code"`
	}
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		fmt.Fprintln(c.out, data)
		return false
	}
	switch eventName {
	case "output":
		fmt.Fprint(c.out, event.Data)
	case "exit":
		fmt.Fprintf(c.out, "\n[terminal exited with code %d]\n", event.Code)
		return true
	default:
		if strings.TrimSpace(event.Data) != "" {
			fmt.Fprintln(c.out, event.Data)
		}
	}
	return false
}

func (c cli) do(method, endpoint string, body any) error {
	return c.doAt(c.base, method, endpoint, body)
}

func (c cli) doAt(base, method, endpoint string, body any) error {
	out, err := c.requestAt(base, method, endpoint, body)
	if err != nil {
		return err
	}
	return c.printResponse(out)
}

func (c cli) request(method, endpoint string, body any) ([]byte, error) {
	return c.requestAt(c.base, method, endpoint, body)
}

func (c cli) requestAt(base, method, endpoint string, body any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, strings.TrimRight(base, "/")+endpoint, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: %s", resp.Status, responseError(out))
	}
	return out, nil
}

func (c cli) printResponse(out []byte) error {
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) == 0 {
		return nil
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, trimmed, "", "  ") == nil {
		if _, err := pretty.WriteTo(c.out); err != nil {
			return err
		}
		_, err := fmt.Fprintln(c.out)
		return err
	}
	_, err := fmt.Fprintln(c.out, string(trimmed))
	return err
}

func responseError(out []byte) string {
	trimmed := strings.TrimSpace(string(out))
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(out, &payload); err == nil && strings.TrimSpace(payload.Error) != "" {
		return strings.TrimSpace(payload.Error)
	}
	if trimmed == "" {
		return "empty response body"
	}
	return trimmed
}

func retryArgs(args []string) (string, string) {
	if len(args) == 0 {
		return "", ""
	}
	if isBackend(args[0]) {
		return commandWord(args[0]), strings.Join(args[1:], " ")
	}
	return "", strings.Join(args, " ")
}

func isBackend(value string) bool {
	switch commandWord(value) {
	case "codex", "claude", "gemini":
		return true
	default:
		return false
	}
}

func commandWord(value string) string {
	return strings.ToLower(strings.Trim(value, " \t\r\n:.,!?"))
}

func path(parts ...string) string {
	escaped := make([]string, 0, len(parts))
	for _, part := range parts {
		escaped = append(escaped, url.PathEscape(part))
	}
	return "/" + strings.Join(escaped, "/")
}

func withAction(action string, args []string) []string {
	out := make([]string, 0, len(args)+1)
	out = append(out, action)
	out = append(out, args...)
	return out
}

func envDefault(getenv func(string) string, key, fallback string) string {
	if value := strings.TrimSpace(getenv(key)); value != "" {
		return value
	}
	return fallback
}

func usage(out io.Writer) {
	fmt.Fprintln(out, `usage:
  homelabctl [-addr http://127.0.0.1:18080] health
  homelabctl [-addr http://127.0.0.1:18080] shell
  homelabctl [-addr http://127.0.0.1:18080] message <text>
  homelabctl [-addr http://127.0.0.1:18080] chat clear <conversation_id>|--all

  homelabctl [-addr http://127.0.0.1:18080] task new [--attach <path>] [--auto|--local|--project <project>|--agent <agent_id> --workdir <workdir_id>|--workdir-path <path> --backend <backend>] <goal>
  homelabctl [-addr http://127.0.0.1:18080] task list
  homelabctl [-addr http://127.0.0.1:18080] task show <task_id>
  homelabctl [-addr http://127.0.0.1:18080] task runs <task_id>
  homelabctl [-addr http://127.0.0.1:18080] task diff <task_id>
  homelabctl [-addr http://127.0.0.1:18080] task run <task_id>
  homelabctl [-addr http://127.0.0.1:18080] task review <task_id>
  homelabctl [-addr http://127.0.0.1:18080] task review-ui <task_id>
  homelabctl [-addr http://127.0.0.1:18080] task queue <task_id> <up|down>
  homelabctl [-addr http://127.0.0.1:18080] task accept <task_id>
  homelabctl [-addr http://127.0.0.1:18080] task restart <task_id>
  homelabctl [-addr http://127.0.0.1:18080] task reopen <task_id> [reason]
  homelabctl [-addr http://127.0.0.1:18080] task cancel <task_id>
  homelabctl [-addr http://127.0.0.1:18080] task retry <task_id> [codex|claude|gemini] [instruction]
  homelabctl [-addr http://127.0.0.1:18080] task delete <task_id>

  homelabctl [-addr http://127.0.0.1:18080] knowledge list
  homelabctl [-addr http://127.0.0.1:18080] knowledge show <space_id>
  homelabctl [-addr http://127.0.0.1:18080] knowledge create [--objective TEXT] [--description TEXT] [--created-by NAME] <title>
  homelabctl [-addr http://127.0.0.1:18080] knowledge update <space_id> [--title TITLE] [--objective TEXT] [--description TEXT]
  homelabctl [-addr http://127.0.0.1:18080] knowledge delete <space_id>
  homelabctl [-addr http://127.0.0.1:18080] knowledge source add <space_id> [--kind text|url|file|note|email|mcp] [--url URL|--uri URI] [--file PATH|--content TEXT] [title]
  homelabctl [-addr http://127.0.0.1:18080] knowledge source delete <space_id> <source_id>
  homelabctl [-addr http://127.0.0.1:18080] knowledge query <space_id> [--limit N] [--source SOURCE_ID]... <query>
  homelabctl [-addr http://127.0.0.1:18080] knowledge ask <space_id> [--limit N] [--source SOURCE_ID]... <question>
  homelabctl [-addr http://127.0.0.1:18080] knowledge research <space_id> [--mode research|brief|study] [--source SOURCE_ID]... <question>
  homelabctl [-addr http://127.0.0.1:18080] knowledge research-run <space_id> [--depth quick|standard|deep] [--scope TEXT] [--mode research|brief|study] [--discover] [--source SOURCE_ID]... <objective>
  homelabctl [-addr http://127.0.0.1:18080] knowledge research-run resume <space_id> <run_id>

  homelabctl [-addr http://127.0.0.1:18080] workflow new <name>: <goal>
  homelabctl [-addr http://127.0.0.1:18080] workflow list
  homelabctl [-addr http://127.0.0.1:18080] workflow show <workflow_id>
  homelabctl [-addr http://127.0.0.1:18080] workflow run <workflow_id>

  homelabctl [-addr http://127.0.0.1:18080] settings
  homelabctl [-addr http://127.0.0.1:18080] settings auto-merge <on|off>

  homelabctl [-addr http://127.0.0.1:18080] assistant list [--all|--archived]
  homelabctl [-addr http://127.0.0.1:18080] assistant show <run_id>
  homelabctl [-addr http://127.0.0.1:18080] assistant archive <run_id> [reason]
  homelabctl [-addr http://127.0.0.1:18080] assistant restore <run_id>
  homelabctl [-addr http://127.0.0.1:18080] assistant signals
  homelabctl [-addr http://127.0.0.1:18080] assistant signal <fingerprint> <useful|dismiss|snooze|create-task> [reason]
  homelabctl [-addr http://127.0.0.1:18080] goal create [--target auto|local|remote] [--project <project>] [--mode guided|autopilot] <objective>

  homelabctl [-addr http://127.0.0.1:18080] agent list
  homelabctl [-addr http://127.0.0.1:18080] agent show <agent_id>
  homelabctl [-addr http://127.0.0.1:18080] workspace list

  homelabctl [-addr http://127.0.0.1:18080] approval list
  homelabctl [-addr http://127.0.0.1:18080] approval approve <approval_id>
  homelabctl [-addr http://127.0.0.1:18080] approval deny <approval_id>
  homelabctl [-addr http://127.0.0.1:18080] approval edit <approval_id> '<json_args>'
  homelabctl [-addr http://127.0.0.1:18080] events [-limit N] [YYYY-MM-DD]
  homelabctl [-healthd-addr http://127.0.0.1:18081] healthd errors [-limit N] [-source SOURCE] [app]
  homelabctl [-supervisord-addr http://127.0.0.1:18082] supervisor status
  homelabctl [-supervisord-addr http://127.0.0.1:18082] supervisor apps
  homelabctl [-supervisord-addr http://127.0.0.1:18082] supervisor start <app>
  homelabctl [-supervisord-addr http://127.0.0.1:18082] supervisor stop [app]
  homelabctl [-supervisord-addr http://127.0.0.1:18082] supervisor restart [app]
  homelabctl [-supervisord-addr http://127.0.0.1:18082] supervisor app adopt <app> <pid>

  homelabctl [-addr http://127.0.0.1:18080] terminal start [cwd]
  homelabctl [-addr http://127.0.0.1:18080] terminal show <session_id>
  homelabctl [-addr http://127.0.0.1:18080] terminal stream <session_id>
  homelabctl [-addr http://127.0.0.1:18080] terminal send <session_id> <text>
  homelabctl [-addr http://127.0.0.1:18080] terminal input <session_id> <text>
  homelabctl [-addr http://127.0.0.1:18080] terminal signal <session_id> <interrupt|suspend|terminate>
  homelabctl [-addr http://127.0.0.1:18080] terminal close <session_id>

Top-level shortcuts:
  homelabctl new <goal>
  homelabctl run|review|queue|accept|restart|reopen|cancel|retry|delete <task_id> [...]
  homelabctl kspace list
  homelabctl approve|deny <approval_id>
  homelabctl memories|remember|unlearn ...
  homelabctl errors [-limit N] [app]
  homelabctl supervisor restart dashboard
  homelabctl status|agents|delegate|ux|refresh|diff|test ...`)
}
