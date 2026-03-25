package system

import (
    "context"
    "sync"
    "time"

    "github.com/shirou/gopsutil/v3/cpu"
    "github.com/shirou/gopsutil/v3/disk"
    "github.com/shirou/gopsutil/v3/host"
    "github.com/shirou/gopsutil/v3/load"
    "github.com/shirou/gopsutil/v3/mem"
    gnet "github.com/shirou/gopsutil/v3/net"
)

type CPUStats struct {
    Percent    float64   `json:"percent"`
    PerCore    []float64 `json:"per_core"`
    CoreCount  int       `json:"core_count"`
    FreqMHz    float64   `json:"freq_mhz"`
    FreqMaxMHz float64   `json:"freq_max_mhz"`
    Load1      float64   `json:"load_1"`
    Load5      float64   `json:"load_5"`
    Load15     float64   `json:"load_15"`
}

type RAMStats struct {
    TotalMB     float64 `json:"total_mb"`
    UsedMB      float64 `json:"used_mb"`
    AvailableMB float64 `json:"available_mb"`
    Percent     float64 `json:"percent"`
    BuffersMB   float64 `json:"buffers_mb"`
    CachedMB    float64 `json:"cached_mb"`
}

type SwapStats struct {
    TotalMB float64 `json:"total_mb"`
    UsedMB  float64 `json:"used_mb"`
    Percent float64 `json:"percent"`
}

type DiskStats struct {
    Mount    string  `json:"mount"`
    Device   string  `json:"device"`
    FSType   string  `json:"fstype"`
    TotalGB  float64 `json:"total_gb"`
    UsedGB   float64 `json:"used_gb"`
    FreeGB   float64 `json:"free_gb"`
    Percent  float64 `json:"percent"`
    ReadBPS  float64 `json:"read_bps"`
    WriteBPS float64 `json:"write_bps"`
}

type NetStats struct {
    Iface          string  `json:"iface"`
    BytesSentS     float64 `json:"bytes_sent_s"`
    BytesRecvS     float64 `json:"bytes_recv_s"`
    BytesSentTotal uint64  `json:"bytes_sent_total"`
    BytesRecvTotal uint64  `json:"bytes_recv_total"`
}

type SystemSnapshot struct {
    TS      float64      `json:"ts"`
    CPU     CPUStats     `json:"cpu"`
    RAM     RAMStats     `json:"ram"`
    Swap    SwapStats    `json:"swap"`
    Disk    []DiskStats  `json:"disk"`
    Net     []NetStats   `json:"net"`
    UptimeS uint64       `json:"uptime_s"`
    BootTS  float64      `json:"boot_ts"`
}

// Delta trackers for net and disk I/O
type netEntry  struct{ sent, recv uint64; t time.Time }
type diskEntry struct{ read, write uint64; t time.Time }

var (
    deltaMu  sync.Mutex
    prevNet  = map[string]netEntry{}
    prevDisk = map[string]diskEntry{}
)

var skipFS = map[string]bool{
    "proc": true, "sysfs": true, "devtmpfs": true, "devpts": true,
    "tmpfs": true, "cgroup": true, "cgroup2": true, "pstore": true,
    "bpf": true, "tracefs": true, "debugfs": true, "squashfs": true,
    "securityfs": true, "hugetlbfs": true, "mqueue": true, "autofs": true,
    "fusectl": true, "configfs": true, "efivarfs": true,
}

func r1(v float64) float64 { return float64(int(v*10+0.5)) / 10 }
func r2(v float64) float64 { return float64(int(v*100+0.5)) / 100 }

func TakeSnapshot() SystemSnapshot {
    ctx := context.Background()
    now := time.Now()
    ts := float64(now.UnixNano()) / 1e9

    // CPU — gopsutil tracks delta internally; first call returns 0.0 (same as psutil interval=None)
    perCore, _ := cpu.PercentWithContext(ctx, 0, true)
    aggSlice, _ := cpu.PercentWithContext(ctx, 0, false)
    aggPct := 0.0
    if len(aggSlice) > 0 {
        aggPct = r1(aggSlice[0])
    }
    for i, v := range perCore {
        perCore[i] = r1(v)
    }

    freqMHz, freqMaxMHz := 0.0, 0.0
    if infos, err := cpu.InfoWithContext(ctx); err == nil && len(infos) > 0 {
        freqMHz = r1(infos[0].Mhz)
        freqMaxMHz = r1(infos[0].Mhz) // cpu.Info gives current MHz on Linux from /proc/cpuinfo
    }

    loadAvg, _ := load.AvgWithContext(ctx)
    l1, l5, l15 := 0.0, 0.0, 0.0
    if loadAvg != nil {
        l1, l5, l15 = loadAvg.Load1, loadAvg.Load5, loadAvg.Load15
    }

    cpuStats := CPUStats{
        Percent: aggPct, PerCore: perCore, CoreCount: len(perCore),
        FreqMHz: freqMHz, FreqMaxMHz: freqMaxMHz,
        Load1: l1, Load5: l5, Load15: l15,
    }

    // RAM
    var ramStats RAMStats
    var swapStats SwapStats
    if vm, err := mem.VirtualMemoryWithContext(ctx); err == nil {
        mb := func(b uint64) float64 { return r1(float64(b) / 1e6) }
        ramStats = RAMStats{
            TotalMB:     mb(vm.Total),
            UsedMB:      mb(vm.Used),
            AvailableMB: mb(vm.Available),
            Percent:     r1(vm.UsedPercent),
            BuffersMB:   mb(vm.Buffers),
            CachedMB:    mb(vm.Cached),
        }
    }
    if sw, err := mem.SwapMemoryWithContext(ctx); err == nil {
        swapStats = SwapStats{
            TotalMB: r1(float64(sw.Total) / 1e6),
            UsedMB:  r1(float64(sw.Used) / 1e6),
            Percent: r1(sw.UsedPercent),
        }
    }

    // Disk
    var diskStats []DiskStats
    ioCounters, _ := disk.IOCountersWithContext(ctx)
    parts, _ := disk.PartitionsWithContext(ctx, false)

    deltaMu.Lock()
    for _, p := range parts {
        if skipFS[p.Fstype] {
            continue
        }
        usage, err := disk.UsageWithContext(ctx, p.Mountpoint)
        if err != nil {
            continue
        }
        devName := p.Device
        for i := len(devName) - 1; i >= 0; i-- {
            if devName[i] == '/' {
                devName = devName[i+1:]
                break
            }
        }

        readBPS, writeBPS := 0.0, 0.0
        if io, ok := ioCounters[devName]; ok {
            if prev, ok := prevDisk[devName]; ok {
                dt := now.Sub(prev.t).Seconds()
                if dt > 0 {
                    readBPS  = float64(io.ReadBytes-prev.read)   / dt
                    writeBPS = float64(io.WriteBytes-prev.write) / dt
                }
            }
            prevDisk[devName] = diskEntry{read: io.ReadBytes, write: io.WriteBytes, t: now}
        }

        diskStats = append(diskStats, DiskStats{
            Mount: p.Mountpoint, Device: p.Device, FSType: p.Fstype,
            TotalGB: r2(float64(usage.Total) / 1e9), UsedGB: r2(float64(usage.Used) / 1e9),
            FreeGB:  r2(float64(usage.Free)  / 1e9), Percent: r1(usage.UsedPercent),
            ReadBPS: readBPS, WriteBPS: writeBPS,
        })
    }

    // Net
    var netStats []NetStats
    counters, _ := gnet.IOCountersWithContext(ctx, true)
    for _, c := range counters {
        sentS, recvS := 0.0, 0.0
        if prev, ok := prevNet[c.Name]; ok {
            dt := now.Sub(prev.t).Seconds()
            if dt > 0 {
                sentS = float64(c.BytesSent-prev.sent) / dt
                recvS = float64(c.BytesRecv-prev.recv) / dt
            }
        }
        prevNet[c.Name] = netEntry{sent: c.BytesSent, recv: c.BytesRecv, t: now}
        netStats = append(netStats, NetStats{
            Iface: c.Name, BytesSentS: sentS, BytesRecvS: recvS,
            BytesSentTotal: c.BytesSent, BytesRecvTotal: c.BytesRecv,
        })
    }
    deltaMu.Unlock()

    uptime, _ := host.UptimeWithContext(ctx)
    bootTime, _ := host.BootTimeWithContext(ctx)

    return SystemSnapshot{
        TS: ts, CPU: cpuStats, RAM: ramStats, Swap: swapStats,
        Disk: diskStats, Net: netStats,
        UptimeS: uptime, BootTS: float64(bootTime),
    }
}
