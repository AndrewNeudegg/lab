package control

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/andrewneudegg/lab/pkg/id"
)

const (
	defaultTerminalCols = 100
	defaultTerminalRows = 30
	maxTerminalCols     = 300
	maxTerminalRows     = 120
	terminalTmuxSocket  = "homelab-web-terminal"
	terminalTmuxPrefix  = "homelab_"
)

type terminalManager struct {
	mu       sync.Mutex
	sessions map[string]*terminalSession
}

type terminalSize struct {
	Cols int
	Rows int
}

type terminalSession struct {
	id      string
	shell   string
	cwd     string
	tmux    string
	cmd     *exec.Cmd
	pty     *os.File
	created time.Time

	writeMu           sync.Mutex
	mu                sync.Mutex
	closed            bool
	size              terminalSize
	inputProbePending string

	exitCode  int
	history   []terminalEvent
	listeners map[chan terminalEvent]struct{}
}

type terminalEvent struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Code int    `json:"code,omitempty"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

func newTerminalManager() *terminalManager {
	return &terminalManager{sessions: make(map[string]*terminalSession)}
}

func (m *terminalManager) create(cwd string) (*terminalSession, error) {
	return m.createWithSize(cwd, terminalSize{Cols: defaultTerminalCols, Rows: defaultTerminalRows})
}

func (m *terminalManager) createWithSize(cwd string, size terminalSize) (*terminalSession, error) {
	if runtime.GOOS == "windows" {
		return nil, errors.New("web terminal PTY sessions are not supported on windows")
	}
	cleanCWD, err := terminalCWD(cwd)
	if err != nil {
		return nil, err
	}
	sessionID := id.New("term")
	shell := terminalShell()
	size = normalizeTerminalSize(size)

	if terminalTmuxAvailable() {
		tmuxName, err := startTerminalTmuxSession(sessionID, cleanCWD, shell)
		if err != nil {
			return nil, err
		}
		return m.attachTmuxSession(sessionID, tmuxName, cleanCWD, shell, size)
	}
	return m.createDirectWithSize(sessionID, cleanCWD, shell, size)
}

func (m *terminalManager) createDirectWithSize(sessionID, cleanCWD, shell string, size terminalSize) (*terminalSession, error) {
	cmd := terminalCommand(shell)
	cmd.Dir = cleanCWD
	cmd.Env = terminalEnv()

	ptyFile, ttyFile, err := openPTY(size)
	if err != nil {
		return nil, err
	}
	defer ttyFile.Close()
	cmd.Stdin = ttyFile
	cmd.Stdout = ttyFile
	cmd.Stderr = ttyFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		_ = ptyFile.Close()
		return nil, err
	}

	session := &terminalSession{
		id:        sessionID,
		shell:     shell,
		cwd:       cleanCWD,
		pty:       ptyFile,
		cmd:       cmd,
		created:   time.Now().UTC(),
		size:      size,
		exitCode:  -1,
		listeners: make(map[chan terminalEvent]struct{}),
	}

	m.mu.Lock()
	m.sessions[session.id] = session
	m.mu.Unlock()

	go session.copyOutput()
	go func() {
		err := cmd.Wait()
		code := 0
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else if err != nil {
			code = 1
		}
		_ = ptyFile.Close()
		session.close(code)

		m.mu.Lock()
		delete(m.sessions, session.id)
		m.mu.Unlock()
	}()

	return session, nil
}

func (m *terminalManager) attachTmuxSession(sessionID, tmuxName, cwd, shell string, size terminalSize) (*terminalSession, error) {
	ptyFile, ttyFile, err := openPTY(size)
	if err != nil {
		return nil, err
	}
	defer ttyFile.Close()

	cmd := terminalTmuxCommand("attach-session", "-t", tmuxName)
	cmd.Stdin = ttyFile
	cmd.Stdout = ttyFile
	cmd.Stderr = ttyFile
	cmd.Env = terminalEnv()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		_ = ptyFile.Close()
		return nil, err
	}

	if cwd == "" {
		cwd = terminalTmuxPaneValue(tmuxName, "#{pane_current_path}")
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if shell == "" {
		shell = terminalTmuxPaneValue(tmuxName, "#{pane_current_command}")
	}
	if shell == "" {
		shell = terminalShell()
	}

	session := &terminalSession{
		id:        sessionID,
		shell:     shell,
		cwd:       cwd,
		tmux:      tmuxName,
		pty:       ptyFile,
		cmd:       cmd,
		created:   time.Now().UTC(),
		size:      size,
		exitCode:  -1,
		listeners: make(map[chan terminalEvent]struct{}),
	}

	m.mu.Lock()
	m.sessions[session.id] = session
	m.mu.Unlock()

	go session.copyOutput()
	go func() {
		err := cmd.Wait()
		code := 0
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else if err != nil {
			code = 1
		}
		_ = ptyFile.Close()
		session.close(code)

		m.mu.Lock()
		if m.sessions[session.id] == session {
			delete(m.sessions, session.id)
		}
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

func (m *terminalManager) getOrAttach(sessionID string, size terminalSize) (*terminalSession, bool, error) {
	if session, ok := m.get(sessionID); ok {
		return session, true, nil
	}
	if runtime.GOOS == "windows" || !terminalTmuxAvailable() {
		return nil, false, nil
	}
	tmuxName := tmuxSessionName(sessionID)
	if !terminalTmuxSessionExists(tmuxName) {
		return nil, false, nil
	}
	session, err := m.attachTmuxSession(
		sessionID,
		tmuxName,
		terminalTmuxPaneValue(tmuxName, "#{pane_current_path}"),
		terminalTmuxPaneValue(tmuxName, "#{pane_current_command}"),
		normalizeTerminalSize(size),
	)
	if err != nil {
		return nil, false, err
	}
	return session, true, nil
}

func (m *terminalManager) close(sessionID string) bool {
	session, ok := m.get(sessionID)
	if !ok {
		if terminalTmuxAvailable() && killTerminalTmuxSession(tmuxSessionName(sessionID)) == nil {
			return true
		}
		return false
	}
	session.terminate()
	return true
}

func terminalCWD(cwd string) (string, error) {
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	return filepath.Abs(cwd)
}

func terminalShell() string {
	if shell := os.Getenv("SHELL"); terminalShellHasLineEditing(shell) {
		return shell
	}
	for _, shell := range []string{"/run/current-system/sw/bin/bash", "/bin/bash"} {
		if terminalShellHasLineEditing(shell) {
			return shell
		}
	}
	if shell, err := exec.LookPath("bash"); err == nil && terminalShellHasLineEditing(shell) {
		return shell
	}
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}
	return "/bin/sh"
}

func terminalShellHasLineEditing(shell string) bool {
	if shell == "" {
		return false
	}
	if _, err := os.Stat(shell); err != nil {
		return false
	}
	if filepath.Base(shell) != "bash" {
		return true
	}
	out, err := exec.Command(shell, "-c", "enable -a").Output()
	return err == nil && strings.Contains(string(out), "enable bind")
}

func terminalCommand(shell string) *exec.Cmd {
	if filepath.Base(shell) == "bash" {
		return exec.Command(shell, "--noprofile", "--norc", "-i")
	}
	return exec.Command(shell, "-i")
}

func terminalEnv() []string {
	env := make([]string, 0, len(os.Environ())+5)
	for _, value := range os.Environ() {
		if strings.HasPrefix(value, "TMUX=") {
			continue
		}
		env = append(env, value)
	}
	return append(env,
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"HOMELAB_WEB_TERMINAL=1",
		"PS1=\\u@\\h:\\w\\$ ",
		"PROMPT_COMMAND=",
	)
}

func terminalTmuxAvailable() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

func terminalTmuxCommand(args ...string) *exec.Cmd {
	fullArgs := append([]string{"-L", terminalTmuxSocket}, args...)
	cmd := exec.Command("tmux", fullArgs...)
	cmd.Env = terminalEnv()
	return cmd
}

func startTerminalTmuxSession(sessionID, cwd, shell string) (string, error) {
	tmuxName := tmuxSessionName(sessionID)
	if terminalTmuxSessionExists(tmuxName) {
		return tmuxName, nil
	}
	cmd := terminalTmuxCommand("new-session", "-d", "-s", tmuxName, "-c", cwd, terminalShellCommand(shell))
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("start tmux session: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if out, err := terminalTmuxCommand("set-option", "-t", tmuxName, "status", "off").CombinedOutput(); err != nil {
		_ = killTerminalTmuxSession(tmuxName)
		return "", fmt.Errorf("configure tmux status: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if out, err := terminalTmuxCommand("set-option", "-s", "escape-time", "0").CombinedOutput(); err != nil {
		_ = killTerminalTmuxSession(tmuxName)
		return "", fmt.Errorf("configure tmux escape-time: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return tmuxName, nil
}

func terminalShellCommand(shell string) string {
	quoted := shellQuote(shell)
	if filepath.Base(shell) == "bash" {
		return quoted + " --noprofile --norc -i"
	}
	return quoted + " -i"
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func tmuxSessionName(sessionID string) string {
	var b strings.Builder
	b.WriteString(terminalTmuxPrefix)
	for _, ch := range sessionID {
		switch {
		case ch >= 'a' && ch <= 'z':
			b.WriteRune(ch)
		case ch >= 'A' && ch <= 'Z':
			b.WriteRune(ch)
		case ch >= '0' && ch <= '9':
			b.WriteRune(ch)
		case ch == '_' || ch == '-':
			b.WriteRune(ch)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func terminalTmuxSessionExists(tmuxName string) bool {
	return terminalTmuxCommand("has-session", "-t", tmuxName).Run() == nil
}

func terminalTmuxPaneValue(tmuxName, format string) string {
	out, err := terminalTmuxCommand("display-message", "-p", "-t", tmuxName, format).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func killTerminalTmuxSession(tmuxName string) error {
	return terminalTmuxCommand("kill-session", "-t", tmuxName).Run()
}

func normalizeTerminalSize(size terminalSize) terminalSize {
	if size.Cols <= 0 {
		size.Cols = defaultTerminalCols
	}
	if size.Rows <= 0 {
		size.Rows = defaultTerminalRows
	}
	if size.Cols < 20 {
		size.Cols = 20
	}
	if size.Rows < 5 {
		size.Rows = 5
	}
	if size.Cols > maxTerminalCols {
		size.Cols = maxTerminalCols
	}
	if size.Rows > maxTerminalRows {
		size.Rows = maxTerminalRows
	}
	return size
}

func (s *terminalSession) copyOutput() {
	buf := make([]byte, 8192)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			s.broadcast(terminalEvent{Type: "output", Data: string(buf[:n])})
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				s.broadcast(terminalEvent{Type: "error", Data: err.Error()})
			}
			return
		}
	}
}

func (s *terminalSession) write(data string) error {
	s.mu.Lock()
	closed := s.closed
	tmuxName := s.tmux
	s.mu.Unlock()
	if closed {
		return errors.New("terminal session is closed")
	}
	if tmuxName != "" {
		if key, ok := terminalTmuxInputKey(data); ok {
			return terminalTmuxCommand("send-keys", "-t", tmuxName, key).Run()
		}
		s.writeMu.Lock()
		data, s.inputProbePending = terminalFilterProbeResponses(s.inputProbePending, data)
		if data == "" {
			s.writeMu.Unlock()
			return nil
		}
		if key, ok := terminalTmuxInputKey(data); ok {
			s.writeMu.Unlock()
			return terminalTmuxCommand("send-keys", "-t", tmuxName, key).Run()
		}
		_, err := io.WriteString(s.pty, data)
		s.writeMu.Unlock()
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err := io.WriteString(s.pty, data)
	return err
}

func terminalTmuxInputKey(data string) (string, bool) {
	switch data {
	case "\x03":
		return "C-c", true
	case "\x04":
		return "C-d", true
	case "\x1a":
		return "C-z", true
	case "\t":
		return "Tab", true
	case "\x1b":
		return "Escape", true
	case "\x1b[D", "\x1bOD":
		return "Left", true
	case "\x1b[C", "\x1bOC":
		return "Right", true
	case "\x1b[A", "\x1bOA":
		return "Up", true
	case "\x1b[B", "\x1bOB":
		return "Down", true
	case "\x1b[1;3D":
		return "M-Left", true
	case "\x1b[1;3C":
		return "M-Right", true
	case "\x1b[1;3A":
		return "M-Up", true
	case "\x1b[1;3B":
		return "M-Down", true
	default:
		return "", false
	}
}

func terminalProbeResponse(data string) bool {
	if data == "" {
		return false
	}
	filtered, pending := terminalFilterProbeResponses("", data)
	return filtered == "" && pending == ""
}

func terminalFilterProbeResponses(pending, data string) (string, string) {
	input := pending + data
	var out strings.Builder
	for i := 0; i < len(input); {
		consumed, complete, probe := consumeTerminalProbeResponse(input[i:])
		if probe {
			if !complete {
				return out.String(), input[i:]
			}
			i += consumed
			continue
		}
		out.WriteByte(input[i])
		i++
	}
	return out.String(), ""
}

func consumeTerminalProbeResponse(value string) (int, bool, bool) {
	if value == "" || value[0] != '\x1b' {
		return 0, false, false
	}
	for _, prefix := range []string{"\x1b[?", "\x1b[>", "\x1b]10;", "\x1b]11;", "\x1bP>|"} {
		if len(value) < len(prefix) && strings.HasPrefix(prefix, value) {
			return 0, false, true
		}
	}
	if strings.HasPrefix(value, "\x1b[?") || strings.HasPrefix(value, "\x1b[>") {
		return consumeTerminalCSIProbeResponse(value)
	}
	if strings.HasPrefix(value, "\x1b]10;") || strings.HasPrefix(value, "\x1b]11;") {
		next, ok := consumeTerminalString(value)
		if !ok {
			return 0, false, true
		}
		return len(value) - len(next), true, true
	}
	if strings.HasPrefix(value, "\x1bP>|") {
		end := strings.Index(value, "\x1b\\")
		if end < 0 {
			return 0, false, true
		}
		return end + 2, true, true
	}
	return 0, false, false
}

func consumeTerminalCSIProbeResponse(value string) (int, bool, bool) {
	i := len("\x1b[?")
	for ; i < len(value); i++ {
		ch := value[i]
		if ch == 'c' {
			return i + 1, true, true
		}
		if (ch < '0' || ch > '9') && ch != ';' {
			return 0, false, false
		}
	}
	return 0, false, true
}

func consumeTerminalString(value string) (string, bool) {
	if idx := strings.IndexByte(value, '\a'); idx >= 0 {
		return value[idx+1:], true
	}
	if idx := strings.Index(value, "\x1b\\"); idx >= 0 {
		return value[idx+2:], true
	}
	return "", false
}

func (s *terminalSession) resize(size terminalSize) error {
	size = normalizeTerminalSize(size)
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return errors.New("terminal session is closed")
	}
	if err := setPTYSize(s.pty, size); err != nil {
		return err
	}
	s.mu.Lock()
	s.size = size
	s.mu.Unlock()
	s.broadcast(terminalEvent{Type: "resize", Cols: size.Cols, Rows: size.Rows})
	return nil
}

func (s *terminalSession) signal(name string) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("signal %q is not supported on windows", name)
	}
	switch name {
	case "interrupt":
		return s.write("\x03")
	case "suspend":
		return s.write("\x1a")
	case "terminate":
		if s.tmux != "" {
			return killTerminalTmuxSession(s.tmux)
		}
		return syscall.Kill(-s.cmd.Process.Pid, syscall.SIGTERM)
	default:
		return fmt.Errorf("unknown signal %q", name)
	}
}

func (s *terminalSession) terminate() {
	_ = s.signal("terminate")
	_ = s.pty.Close()
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
	if len(s.history) > 500 {
		s.history = s.history[len(s.history)-500:]
	}
	for ch := range s.listeners {
		select {
		case ch <- event:
		default:
		}
	}
}

type ptyWinsize struct {
	Rows   uint16
	Cols   uint16
	XPixel uint16
	YPixel uint16
}

func openPTY(size terminalSize) (*os.File, *os.File, error) {
	masterFD, err := syscall.Open("/dev/ptmx", syscall.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, err
	}
	master := os.NewFile(uintptr(masterFD), "/dev/ptmx")
	unlock := 0
	if err := ioctl(masterFD, syscall.TIOCSPTLCK, unsafe.Pointer(&unlock)); err != nil {
		_ = master.Close()
		return nil, nil, err
	}
	var ptyNumber uint32
	if err := ioctl(masterFD, syscall.TIOCGPTN, unsafe.Pointer(&ptyNumber)); err != nil {
		_ = master.Close()
		return nil, nil, err
	}
	slavePath := fmt.Sprintf("/dev/pts/%d", ptyNumber)
	slaveFD, err := syscall.Open(slavePath, syscall.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		_ = master.Close()
		return nil, nil, err
	}
	slave := os.NewFile(uintptr(slaveFD), slavePath)
	if err := setPTYSize(slave, size); err != nil {
		_ = master.Close()
		_ = slave.Close()
		return nil, nil, err
	}
	return master, slave, nil
}

func setPTYSize(file *os.File, size terminalSize) error {
	size = normalizeTerminalSize(size)
	winsize := ptyWinsize{Rows: uint16(size.Rows), Cols: uint16(size.Cols)}
	return ioctl(int(file.Fd()), syscall.TIOCSWINSZ, unsafe.Pointer(&winsize))
}

func ioctl(fd int, request uintptr, arg unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), request, uintptr(arg))
	if errno != 0 {
		return errno
	}
	return nil
}
