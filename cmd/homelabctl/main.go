package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

func main() {
	addr := flag.String("addr", getenv("HOMELABD_ADDR", "http://127.0.0.1:8080"), "homelabd base URL")
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	client := client{base: strings.TrimRight(*addr, "/"), http: http.DefaultClient}
	var err error
	switch args[0] {
	case "message":
		err = client.message(strings.Join(args[1:], " "))
	case "task":
		err = client.task(args[1:])
	case "approval":
		err = client.approval(args[1:])
	case "events":
		err = client.events(args[1:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "homelabctl:", err)
		os.Exit(1)
	}
}

type client struct {
	base string
	http *http.Client
}

func (c client) task(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: homelabctl task <new|list|show|run|review>")
	}
	switch args[0] {
	case "new":
		goal := strings.Join(args[1:], " ")
		if goal == "" {
			return fmt.Errorf("usage: homelabctl task new <goal>")
		}
		return c.do(http.MethodPost, "/tasks", map[string]any{"goal": goal})
	case "list":
		return c.do(http.MethodGet, "/tasks", nil)
	case "show":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl task show <task_id>")
		}
		return c.do(http.MethodGet, "/tasks/"+args[1], nil)
	case "run":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl task run <task_id>")
		}
		return c.do(http.MethodPost, "/tasks/"+args[1]+"/run", nil)
	case "review":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl task review <task_id>")
		}
		return c.do(http.MethodPost, "/tasks/"+args[1]+"/review", nil)
	default:
		return fmt.Errorf("unknown task command %q", args[0])
	}
}

func (c client) approval(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: homelabctl approval <list|approve|deny>")
	}
	switch args[0] {
	case "list":
		return c.do(http.MethodGet, "/approvals", nil)
	case "approve":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl approval approve <approval_id>")
		}
		return c.do(http.MethodPost, "/approvals/"+args[1]+"/approve", nil)
	case "deny":
		if len(args) != 2 {
			return fmt.Errorf("usage: homelabctl approval deny <approval_id>")
		}
		return c.do(http.MethodPost, "/approvals/"+args[1]+"/deny", nil)
	default:
		return fmt.Errorf("unknown approval command %q", args[0])
	}
}

func (c client) events(args []string) error {
	path := "/events"
	if len(args) > 0 {
		path += "?date=" + args[0]
	}
	return c.do(http.MethodGet, path, nil)
}

func (c client) message(message string) error {
	if message == "" {
		return fmt.Errorf("usage: homelabctl message <text>")
	}
	return c.do(http.MethodPost, "/message", map[string]any{"from": "homelabctl", "content": message})
}

func (c client) do(method, path string, body any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(out)))
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, out, "", "  ") == nil {
		_, err = pretty.WriteTo(os.Stdout)
		if err == nil {
			fmt.Println()
		}
		return err
	}
	fmt.Println(string(out))
	return nil
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  homelabctl [-addr http://127.0.0.1:8080] message <text>
  homelabctl [-addr http://127.0.0.1:8080] task new <goal>
  homelabctl [-addr http://127.0.0.1:8080] task list
  homelabctl [-addr http://127.0.0.1:8080] task show <task_id>
  homelabctl [-addr http://127.0.0.1:8080] task run <task_id>
  homelabctl [-addr http://127.0.0.1:8080] task review <task_id>
  homelabctl [-addr http://127.0.0.1:8080] approval list
  homelabctl [-addr http://127.0.0.1:8080] approval approve <approval_id>
  homelabctl [-addr http://127.0.0.1:8080] approval deny <approval_id>
  homelabctl [-addr http://127.0.0.1:8080] events [YYYY-MM-DD]`)
}
