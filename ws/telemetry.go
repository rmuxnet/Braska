package ws

import (
    "encoding/json"
    "log/slog"
    "net/http"
    "time"

    "github.com/gorilla/websocket"

    "braska/internal/auth"
    "braska/internal/sysfs"
    "braska/internal/system"
)

var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool { return true },
}

func TelemetryHandler(token string) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if !auth.VerifyToken(r.URL.Query().Get("token"), token) {
            http.Error(w, "Unauthorized", http.StatusUnauthorized)
            return
        }
        conn, err := upgrader.Upgrade(w, r, nil)
        if err != nil {
            slog.Error("telemetry upgrade failed", "err", err)
            return
        }
        defer conn.Close()
        slog.Info("telemetry connected", "remote", r.RemoteAddr)

        ticker := time.NewTicker(time.Second)
        defer ticker.Stop()

        for range ticker.C {
            snap := system.TakeSnapshot()
            fan, fanErr := sysfs.ReadFanTelemetry()
            tunnel := system.GetTunnelManager().StatusDict()

            var frame map[string]any
            if fanErr != nil {
                frame = map[string]any{"error": fanErr.Error(), "ts": snap.TS}
            } else {
                frame = map[string]any{
                    "ts":       snap.TS,
                    "fan":      fan,
                    "cpu":      snap.CPU,
                    "ram":      snap.RAM,
                    "swap":     snap.Swap,
                    "disk":     snap.Disk,
                    "net":      snap.Net,
                    "uptime_s": snap.UptimeS,
                    "tunnel":   tunnel,
                }
            }

            data, _ := json.Marshal(frame)
            if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
                slog.Info("telemetry disconnected", "remote", r.RemoteAddr)
                return
            }
        }
    }
}
