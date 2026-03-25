package system

import (
    "context"
    "log/slog"
    "sort"
    "strings"
    "syscall"

    "github.com/shirou/gopsutil/v3/process"
)

type ProcessInfo struct {
    PID      int32   `json:"pid"`
    Name     string  `json:"name"`
    User     string  `json:"user"`
    Status   string  `json:"status"`
    CPUPct   float64 `json:"cpu_pct"`
    MemRSSMB float64 `json:"mem_rss_mb"`
    MemPct   float32 `json:"mem_pct"`
    Threads  int32   `json:"threads"`
    Cmdline  string  `json:"cmdline"`
}

func ListProcesses(limit int, sortBy string) []ProcessInfo {
    ctx := context.Background()
    pids, err := process.PidsWithContext(ctx)
    if err != nil {
        return nil
    }

    var procs []ProcessInfo
    for _, pid := range pids {
        p, err := process.NewProcessWithContext(ctx, pid)
        if err != nil {
            continue
        }
        info := processInfo(p)
        if info != nil {
            procs = append(procs, *info)
        }
    }

    switch sortBy {
    case "mem":
        sort.Slice(procs, func(i, j int) bool { return procs[i].MemRSSMB > procs[j].MemRSSMB })
    case "pid":
        sort.Slice(procs, func(i, j int) bool { return procs[i].PID < procs[j].PID })
    case "name":
        sort.Slice(procs, func(i, j int) bool {
            return strings.ToLower(procs[i].Name) < strings.ToLower(procs[j].Name)
        })
    default: // cpu
        sort.Slice(procs, func(i, j int) bool { return procs[i].CPUPct > procs[j].CPUPct })
    }

    if limit > 0 && len(procs) > limit {
        procs = procs[:limit]
    }
    return procs
}

func processInfo(p *process.Process) *ProcessInfo {
    name, err := p.Name()
    if err != nil {
        return nil
    }
    cpuPct, _ := p.CPUPercent()
    memInfo, _ := p.MemoryInfo()
    memPct, _  := p.MemoryPercent()
    statuses, _ := p.Status()
    username, _ := p.Username()
    threads, _  := p.NumThreads()
    cmdline, _  := p.Cmdline()

    rssMB := 0.0
    if memInfo != nil {
        rssMB = r1(float64(memInfo.RSS) / 1e6)
    }
    if len(cmdline) > 120 {
        cmdline = cmdline[:120]
    }
    status := "unknown"
    if len(statuses) > 0 {
        status = statuses[0]
    }

    return &ProcessInfo{
        PID: p.Pid, Name: name, User: username, Status: status,
        CPUPct: r1(cpuPct), MemRSSMB: rssMB, MemPct: memPct,
        Threads: threads, Cmdline: strings.TrimSpace(cmdline),
    }
}

var sigMap = map[string]syscall.Signal{
    "SIGTERM": syscall.SIGTERM,
    "SIGKILL": syscall.SIGKILL,
    "SIGHUP":  syscall.SIGHUP,
    "SIGSTOP": syscall.SIGSTOP,
    "SIGCONT": syscall.SIGCONT,
}

func KillProcess(pid int32, sig string) error {
    signal, ok := sigMap[sig]
    if !ok {
        return fmt.Errorf("unknown signal %q", sig)
    }
    p, err := process.NewProcess(pid)
    if err != nil {
        return err
    }
    if err := p.SendSignal(signal); err != nil {
        return err
    }
    slog.Info("signal sent", "pid", pid, "signal", sig)
    return nil
}
