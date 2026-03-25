package system

import (
    "bufio"
    "fmt"
    "log/slog"
    "os/exec"
    "strings"
    "sync"
)

type TunnelState string

const (
    StateStopped  TunnelState = "stopped"
    StateStarting TunnelState = "starting"
    StateRunning  TunnelState = "running"
    StateError    TunnelState = "error"
)

type TunnelManager struct {
    mu    sync.Mutex
    port  int
    state TunnelState
    url   string
    errMsg string
    cmd   *exec.Cmd
}

func NewTunnelManager(port int) *TunnelManager {
    return &TunnelManager{port: port, state: StateStopped}
}

func (t *TunnelManager) StatusDict() map[string]any {
    t.mu.Lock()
    defer t.mu.Unlock()
    return map[string]any{
        "state": t.state,
        "url":   t.url,
        "port":  t.port,
        "error": t.errMsg,
    }
}

// Start is idempotent — returns existing URL if already running.
func (t *TunnelManager) Start() (string, error) {
    t.mu.Lock()
    if t.state == StateRunning && t.url != "" {
        url := t.url
        t.mu.Unlock()
        return url, nil
    }
    t.state = StateStarting
    t.errMsg = ""
    t.url = ""
    port := t.port
    t.mu.Unlock()

    cmd := exec.Command("cloudflared", "tunnel", "--url",
        fmt.Sprintf("http://localhost:%d", port))
    stderr, err := cmd.StderrPipe()
    if err != nil {
        return "", t.setError(fmt.Errorf("stderr pipe: %w", err))
    }
    if err := cmd.Start(); err != nil {
        return "", t.setError(fmt.Errorf("start cloudflared: %w", err))
    }

    // Scan stderr until we find the https:// URL
    url := ""
    scanner := bufio.NewScanner(stderr)
    for scanner.Scan() {
        line := scanner.Text()
        if idx := strings.Index(line, "https://"); idx >= 0 {
            rest := line[idx:]
            end := strings.IndexAny(rest, " \t\n")
            if end < 0 {
                end = len(rest)
            }
            url = rest[:end]
            break
        }
    }

    if url == "" {
        _ = cmd.Process.Kill()
        return "", t.setError(fmt.Errorf("could not extract tunnel URL from cloudflared output"))
    }

    t.mu.Lock()
    t.url = url
    t.state = StateRunning
    t.cmd = cmd
    t.mu.Unlock()

    slog.Info("cloudflare tunnel up", "url", url)
    return url, nil
}

func (t *TunnelManager) Stop() {
    t.mu.Lock()
    defer t.mu.Unlock()
    if t.cmd != nil && t.cmd.Process != nil {
        _ = t.cmd.Process.Kill()
        _ = t.cmd.Wait()
        t.cmd = nil
    }
    t.state = StateStopped
    t.url = ""
}

func (t *TunnelManager) setError(err error) error {
    t.mu.Lock()
    t.state = StateError
    t.errMsg = err.Error()
    t.mu.Unlock()
    slog.Error("tunnel error", "err", err)
    return err
}

// Package-level singleton
var (
    tunnelOnce sync.Once
    tunnelMgr  *TunnelManager
)

func GetTunnelManager() *TunnelManager {
    tunnelOnce.Do(func() { tunnelMgr = NewTunnelManager(8765) })
    return tunnelMgr
}
