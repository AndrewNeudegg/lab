package control

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	cmd     *exec.Cmd
	pty     *os.File
	created time.Time

	writeMu sync.Mutex
	mu      sync.Mutex
	closed  bool
	size    terminalSize

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
	size = normalizeTerminalSize(size)
	shell := terminalShell()
	cmd := terminalCommand(shell)
	cmd.Dir = cleanCWD
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"HOMELAB_WEB_TERMINAL=1",
		"PS1=\\u@\\h:\\w\\$ ",
		"PROMPT_COMMAND=",
	)

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
		id:        id.New("term"),
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
	if filepath.Base(shell) == "bash" {
		return exec.Command(shell, "--noprofile", "--norc", "-i")
	}
	return exec.Command(shell, "-i")
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
	s.mu.Unlock()
	if closed {
		return errors.New("terminal session is closed")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err := io.WriteString(s.pty, data)
	return err
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
