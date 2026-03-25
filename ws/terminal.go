package ws

import (
    "encoding/json"
    "io"
    "log/slog"
    "net/http"
    "os"
    "os/exec"
    "sync"
    "syscall"

    "github.com/creack/pty"
    "github.com/gorilla/websocket"

    "braska/internal/auth"
)

// ── Persistent PTY session ────────────────────────────────────────────────

type termSession struct {
    ptmx *os.File
    cmd  *exec.Cmd
}

func (s *termSession) alive() bool {
    if s == nil || s.cmd == nil || s.cmd.Process == nil {
        return false
    }
    return s.cmd.Process.Signal(syscall.Signal(0)) == nil
}

var (
    sessionMu sync.Mutex
    session   *termSession
)

func getOrSpawnSession() (*termSession, error) {
    sessionMu.Lock()
    defer sessionMu.Unlock()

    if session.alive() {
        slog.Info("reattaching to existing shell", "pid", session.cmd.Process.Pid)
        return session, nil
    }

    cmd := exec.Command("/bin/bash", "--login")
    cmd.Env = []string{
        "TERM=xterm-256color",
        "HOME=/root",
        "USER=root",
        "SHELL=/bin/bash",
        "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
        "LANG=en_US.UTF-8",
        `PS1=\[\033[1;31m\]root\[\033[0m\]@\[\033[1;36m\]PlayStation4\[\033[0m\]:\[\033[1;34m\]\w\[\033[0m\]\$ `,
    }

    ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
    if err != nil {
        return nil, err
    }

    session = &termSession{ptmx: ptmx, cmd: cmd}
    slog.Info("shell spawned", "pid", cmd.Process.Pid)
    return session, nil
}

// ── WebSocket handler ─────────────────────────────────────────────────────

// wsWriter wraps gorilla conn with a write mutex (gorilla: one concurrent writer).
type wsWriter struct {
    conn *websocket.Conn
    mu   sync.Mutex
}

func (w *wsWriter) write(data []byte) error {
    w.mu.Lock()
    defer w.mu.Unlock()
    return w.conn.WriteMessage(websocket.TextMessage, data)
}

type resizeMsg struct {
    Type string `json:"type"`
    Cols uint16 `json:"cols"`
    Rows uint16 `json:"rows"`
}

func TerminalHandler(token string) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if !auth.VerifyToken(r.URL.Query().Get("token"), token) {
            http.Error(w, "Unauthorized", http.StatusUnauthorized)
            return
        }
        conn, err := upgrader.Upgrade(w, r, nil)
        if err != nil {
            slog.Error("terminal upgrade failed", "err", err)
            return
        }
        defer conn.Close()
        slog.Info("terminal connected", "remote", r.RemoteAddr)

        sess, err := getOrSpawnSession()
        if err != nil {
            slog.Error("spawn session failed", "err", err)
            conn.WriteMessage(websocket.CloseMessage,
                websocket.FormatCloseMessage(1011, err.Error()))
            return
        }

        writer := &wsWriter{conn: conn}
        done := make(chan struct{})

        // PTY → WebSocket
        go func() {
            defer close(done)
            buf := make([]byte, 4096)
            for {
                n, err := sess.ptmx.Read(buf)
                if n > 0 {
                    if err := writer.write(buf[:n]); err != nil {
                        return
                    }
                }
                if err != nil {
                    if err != io.EOF {
                        slog.Debug("pty read ended", "err", err)
                    }
                    return
                }
            }
        }()

        // WebSocket → PTY
        for {
            _, msg, err := conn.ReadMessage()
            if err != nil {
                break
            }
            // Check for resize frame
            if len(msg) > 0 && msg[0] == '{' {
                var rm resizeMsg
                if json.Unmarshal(msg, &rm) == nil && rm.Type == "resize" {
                    _ = pty.Setsize(sess.ptmx, &pty.Winsize{
                        Rows: rm.Rows, Cols: rm.Cols,
                    })
                    continue
                }
            }
            _, _ = sess.ptmx.Write(msg)
        }

        <-done
        slog.Info("terminal disconnected (shell kept alive)", "remote", r.RemoteAddr)
    }
}
