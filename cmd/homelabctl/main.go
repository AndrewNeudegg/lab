package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAddr = "http://127.0.0.1:8080"
	defaultFrom = "homelabctl"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv, nil))
}

type cli struct {
	base string
	http *http.Client
	in   io.Reader
	out  io.Writer
	err  io.Writer
	from string
	json bool
}

func run(args []string, in io.Reader, out, errOut io.Writer, getenv func(string) string, httpClient *http.Client) int {
	if getenv == nil {
		getenv = os.Getenv
	}
	flags := flag.NewFlagSet("homelabctl", flag.ContinueOnError)
	flags.SetOutput(errOut)
	addr := flags.String("addr", envDefault(getenv, "HOMELABD_ADDR", defaultAddr), "homelabd base URL")
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
		base: strings.TrimRight(*addr, "/"),
		http: httpClient,
		in:   in,
		out:  out,
		err:  errOut,
		from: *from,
		json: *jsonOutput,
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
	case "message", "chat", "say", "send":
		return c.message(strings.Join(args[1:], " "))
	case "shell", "interactive", "repl":
		return c.shell()
	case "task":
		return c.task(args[1:])
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
	case "terminal", "term":
		return c.terminal(args[1:])
	case "new":
		return c.task(withAction("new", args[1:]))
	case "show":
		return c.task(withAction("show", args[1:]))
	case "runs":
		return c.task(withAction("runs", args[1:]))
	case "run", "review", "accept", "verify", "reopen", "cancel", "stop", "retry":
		return c.task(withAction(cmd, args[1:]))
	case "status", "agents", "delete", "remove", "rm", "refresh", "rebase", "sync",
		"delegate", "escalate", "codex", "claude", "gemini", "diff", "test", "patch",
		"search", "web", "internet", "research", "read", "reflect", "deep", "work", "start":
		return c.message(strings.Join(args, " "))
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func (c cli) task(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: homelabctl task <new|list|show|runs|run|review|accept|reopen|cancel|retry>")
	}
	action := commandWord(args[0])
	switch action {
	case "new", "create":
		goal := strings.TrimSpace(strings.Join(args[1:], " "))
		if goal == "" {
			return fmt.Errorf("usage: homelabctl task new <goal>")
		}
		return c.do(http.MethodPost, "/tasks", map[string]any{"goal": goal})
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
	case "run", "review":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl task %s <task_id>", action)
		}
		return c.do(http.MethodPost, path("tasks", args[1], action), nil)
	case "accept", "verify":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl task accept <task_id>")
		}
		return c.do(http.MethodPost, path("tasks", args[1], "accept"), nil)
	case "cancel", "stop":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl task cancel <task_id>")
		}
		return c.do(http.MethodPost, path("tasks", args[1], "cancel"), nil)
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

func (c cli) approval(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: homelabctl approval <list|approve|deny>")
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

func (c cli) terminal(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: homelabctl terminal <start|send|input|stream|signal|close>")
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
		Reply string `json:"reply"`
	}
	if err := json.Unmarshal(out, &reply); err == nil && strings.TrimSpace(reply.Reply) != "" {
		fmt.Fprintln(c.out, reply.Reply)
		return nil
	}
	return c.printResponse(out)
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
	out, err := c.request(method, endpoint, body)
	if err != nil {
		return err
	}
	return c.printResponse(out)
}

func (c cli) request(method, endpoint string, body any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+endpoint, reader)
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
  homelabctl [-addr http://127.0.0.1:8080] health
  homelabctl [-addr http://127.0.0.1:8080] shell
  homelabctl [-addr http://127.0.0.1:8080] message <text>

  homelabctl [-addr http://127.0.0.1:8080] task new <goal>
  homelabctl [-addr http://127.0.0.1:8080] task list
  homelabctl [-addr http://127.0.0.1:8080] task show <task_id>
  homelabctl [-addr http://127.0.0.1:8080] task runs <task_id>
  homelabctl [-addr http://127.0.0.1:8080] task run <task_id>
  homelabctl [-addr http://127.0.0.1:8080] task review <task_id>
  homelabctl [-addr http://127.0.0.1:8080] task accept <task_id>
  homelabctl [-addr http://127.0.0.1:8080] task reopen <task_id> [reason]
  homelabctl [-addr http://127.0.0.1:8080] task cancel <task_id>
  homelabctl [-addr http://127.0.0.1:8080] task retry <task_id> [codex|claude|gemini] [instruction]

  homelabctl [-addr http://127.0.0.1:8080] approval list
  homelabctl [-addr http://127.0.0.1:8080] approval approve <approval_id>
  homelabctl [-addr http://127.0.0.1:8080] approval deny <approval_id>
  homelabctl [-addr http://127.0.0.1:8080] events [-limit N] [YYYY-MM-DD]

  homelabctl [-addr http://127.0.0.1:8080] terminal start [cwd]
  homelabctl [-addr http://127.0.0.1:8080] terminal stream <session_id>
  homelabctl [-addr http://127.0.0.1:8080] terminal send <session_id> <text>
  homelabctl [-addr http://127.0.0.1:8080] terminal input <session_id> <text>
  homelabctl [-addr http://127.0.0.1:8080] terminal signal <session_id> <interrupt|suspend|terminate>
  homelabctl [-addr http://127.0.0.1:8080] terminal close <session_id>

Top-level shortcuts:
  homelabctl new <goal>
  homelabctl run|review|accept|reopen|cancel|retry <task_id> [...]
  homelabctl approve|deny <approval_id>
  homelabctl status|agents|delegate|refresh|diff|test|delete ...`)
}
