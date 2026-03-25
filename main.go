package main

import (
    "log/slog"
    "net/http"
    "os"

    "github.com/go-chi/chi/v5"
    "github.com/go-chi/chi/v5/middleware"

    "braska/internal/ws"
)

func main() {
    token := os.Getenv("AUTH_TOKEN")
    if token == "" {
        token = "changeme"
    }

    r := chi.NewRouter()
    r.Use(middleware.Logger)
    r.Use(middleware.Recoverer)

    r.Get("/ws/telemetry", ws.TelemetryHandler(token))
    r.Get("/ws/terminal",  ws.TerminalHandler(token))

    slog.Info("ps4-daemon listening", "addr", ":8765")
    if err := http.ListenAndServe(":8765", r); err != nil {
        slog.Error("server failed", "err", err)
        os.Exit(1)
    }
}
