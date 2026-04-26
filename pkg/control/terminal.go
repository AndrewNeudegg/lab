package control

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/andrewneudegg/lab/pkg/id"
)

type terminalManager struct {
	mu       sync.Mutex
	sessions map[string]*terminalSession
}

type terminalSession struct {
	id      string
	shell   string
	cwd     string
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	created time.Time

	mu        sync.Mutex
	closed    bool
	exitCode  int
	history   []terminalEvent
	listeners map[chan terminalEvent]struct{}
}

type terminalEvent struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Code int    `json:"code,omitempty"`
}

func newTerminalManager() *terminalManager {
	return &terminalManager{sessions: make(map[string]*terminalSession)}
}

func (m *terminalManager) create(cwd string) (*terminalSession, error) {
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	cleanCWD, err := filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}
	shell := terminalShell()
	cmd := terminalCommand(shell)
	cmd.Dir = cleanCWD
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "HOMELAB_WEB_TERMINAL=1")
	if runtime.GOOS != "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	session := &terminalSession{
		id:        id.New("term"),
		shell:     shell,
		cwd:       cleanCWD,
		cmd:       cmd,
		stdin:     stdin,
		created:   time.Now().UTC(),
		exitCode:  -1,
		listeners: make(map[chan terminalEvent]struct{}),
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.sessions[session.id] = session
	m.mu.Unlock()

	var outputDone sync.WaitGroup
	outputDone.Add(2)
	go func() {
		defer outputDone.Done()
		session.copyOutput(stdout)
	}()
	go func() {
		defer outputDone.Done()
		session.copyOutput(stderr)
	}()
	go func() {
		err := cmd.Wait()
		outputDone.Wait()
		code := 0
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else if err != nil {
			code = 1
		}
		session.close(code)

		m.mu.Lock()
		delete(m.sessions, session.id)
		m.mu.Unlock()
	}()

	return session, nil
}

func (m *terminalManager) get(sessionID string) (*terminalSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[sessionID]
	return session, ok
}

func (m *terminalManager) close(sessionID string) bool {
	session, ok := m.get(sessionID)
	if !ok {
		return false
	}
	session.terminate()
	return true
}

func terminalShell() string {
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}
	if _, err := os.Stat("/bin/bash"); err == nil {
		return "/bin/bash"
	}
	return "/bin/sh"
}

func terminalCommand(shell string) *exec.Cmd {
	if runtime.GOOS != "windows" {
		if script, err := exec.LookPath("script"); err == nil {
			return exec.Command(script, "-qfec", strconv.Quote(shell)+" -i", "/dev/null")
		}
	}
	return exec.Command(shell, "-i")
}

func (s *terminalSession) copyOutput(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)
	scanner.Split(scanTerminalChunks)
	for scanner.Scan() {
		s.broadcast(terminalEvent{Type: "output", Data: scanner.Text()})
	}
}

func scanTerminalChunks(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if len(data) > 0 {
		return len(data), data, nil
	}
	if atEOF {
		return 0, nil, nil
	}
	return 0, nil, nil
}

func (s *terminalSession) write(data string) error {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return errors.New("terminal session is closed")
	}
	_, err := io.WriteString(s.stdin, data)
	return err
}

func (s *terminalSession) signal(name string) error {
	if runtime.GOOS == "windows" {
		if name == "interrupt" {
			return s.cmd.Process.Signal(os.Interrupt)
		}
		return fmt.Errorf("signal %q is not supported on windows", name)
	}
	var sig syscall.Signal
	switch name {
	case "interrupt":
		sig = syscall.SIGINT
	case "suspend":
		sig = syscall.SIGTSTP
	case "terminate":
		sig = syscall.SIGTERM
	default:
		return fmt.Errorf("unknown signal %q", name)
	}
	return syscall.Kill(-s.cmd.Process.Pid, sig)
}

func (s *terminalSession) terminate() {
	_ = s.signal("terminate")
	_ = s.stdin.Close()
}

func (s *terminalSession) subscribe() chan terminalEvent {
	ch := make(chan terminalEvent, 256)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		for _, event := range s.history {
			ch <- event
		}
		ch <- terminalEvent{Type: "exit", Code: s.exitCode}
		close(ch)
		return ch
	}
	for _, event := range s.history {
		ch <- event
	}
	s.listeners[ch] = struct{}{}
	return ch
}

func (s *terminalSession) unsubscribe(ch chan terminalEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.listeners[ch]; ok {
		delete(s.listeners, ch)
		close(ch)
	}
}

func (s *terminalSession) close(code int) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.exitCode = code
	listeners := s.listeners
	s.listeners = make(map[chan terminalEvent]struct{})
	s.mu.Unlock()

	event := terminalEvent{Type: "exit", Code: code}
	for ch := range listeners {
		ch <- event
		close(ch)
	}
}

func (s *terminalSession) broadcast(event terminalEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.history = append(s.history, event)
	if len(s.history) > 200 {
		s.history = s.history[len(s.history)-200:]
	}
	for ch := range s.listeners {
		select {
		case ch <- event:
		default:
		}
	}
}
