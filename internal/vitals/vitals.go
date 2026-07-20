// Package vitals collects host metrics on Linux by reading /proc and shelling
// out to a couple of well-known tools. Everything is best-effort: on a box
// without systemd or an NVIDIA GPU the relevant fields just come back empty
// rather than failing the whole snapshot.
package vitals

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/clarkbar-sys/hush/internal/version"
)

// Snapshot is a single point-in-time reading of a machine.
type Snapshot struct {
	Host     string `json:"host"`
	Version  string `json:"version"` // hush-agent build version, e.g. "v1.3.0", "dev-a1b2c3d4e5f6", or "dev"
	OS       string `json:"os"`
	Up       string `json:"up"`
	CPU      int    `json:"cpu"`
	Mem      int    `json:"mem"`
	Disk     int    `json:"disk"`
	GPU      *int   `json:"gpu"`
	VRAM     *int   `json:"vram"`
	GPUName  string `json:"gpuName,omitempty"`
	VRAMText string `json:"vramText,omitempty"`
	Load     string `json:"load"`
	NetRx    int    `json:"netRx"`  // inbound bytes/sec, sampled over the prior ~1s (excludes loopback)
	NetTx    int    `json:"netTx"`  // outbound bytes/sec, sampled over the prior ~1s (excludes loopback)
	Status   string `json:"status"` // good | warn | crit
}

// --- CPU: sampled in the background so /vitals stays instant -----------------

var (
	cpuMu               sync.Mutex
	cpuPct              int
	prevIdle, prevTotal uint64
)

// StartSampler primes and begins the 1s CPU and network sampling loop. Call
// once at boot.
func StartSampler() {
	prevIdle, prevTotal = readCPUTimes()
	prevRxBytes, prevTxBytes = readNetBytes()
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for range t.C {
			idle, total := readCPUTimes()
			dt := total - prevTotal
			pct := 0
			if dt > 0 {
				pct = round(float64(dt-(idle-prevIdle)) / float64(dt) * 100)
			}
			cpuMu.Lock()
			cpuPct = clamp(pct)
			cpuMu.Unlock()
			prevIdle, prevTotal = idle, total

			rxBytes, txBytes := readNetBytes()
			netMu.Lock()
			netRxBps, netTxBps = counterRate(prevRxBytes, rxBytes), counterRate(prevTxBytes, txBytes)
			netMu.Unlock()
			prevRxBytes, prevTxBytes = rxBytes, txBytes
		}
	}()
}

func cpuUsage() int {
	cpuMu.Lock()
	defer cpuMu.Unlock()
	return cpuPct
}

func readCPUTimes() (idle, total uint64) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 5 && fields[0] == "cpu" {
			for i, s := range fields[1:] {
				v, _ := strconv.ParseUint(s, 10, 64)
				total += v
				if i == 3 || i == 4 { // idle + iowait
					idle += v
				}
			}
		}
	}
	return
}

// --- Network: sampled alongside CPU so /vitals stays instant ----------------

var (
	netMu                    sync.Mutex
	netRxBps, netTxBps       int
	prevRxBytes, prevTxBytes uint64
)

func netUsage() (rx, tx int) {
	netMu.Lock()
	defer netMu.Unlock()
	return netRxBps, netTxBps
}

// counterRate turns two readings of a monotonic byte counter one sampler
// tick apart into a bytes/sec rate. A counter that goes backwards (interface
// reset, or the machine's own counters wrapping) reports 0 rather than an
// underflowed huge number.
func counterRate(prev, cur uint64) int {
	if cur <= prev {
		return 0
	}
	return int(cur - prev)
}

// readNetBytes sums rx/tx bytes across every interface in /proc/net/dev
// except loopback, so the figure reflects real network traffic.
func readNetBytes() (rx, tx uint64) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		i := strings.Index(line, ":")
		if i < 0 {
			continue // the two header lines have no ':'
		}
		if strings.TrimSpace(line[:i]) == "lo" {
			continue
		}
		fields := strings.Fields(line[i+1:])
		if len(fields) < 9 {
			continue
		}
		if v, err := strconv.ParseUint(fields[0], 10, 64); err == nil {
			rx += v
		}
		if v, err := strconv.ParseUint(fields[8], 10, 64); err == nil {
			tx += v
		}
	}
	return rx, tx
}

// --- Memory / disk / load / uptime / os -------------------------------------

func memUsage() int {
	info := readMeminfo()
	total := info["MemTotal"]
	if total == 0 {
		return 0
	}
	used := total - info["MemAvailable"]
	return clamp(round(float64(used) / float64(total) * 100))
}

func readMeminfo() map[string]uint64 {
	res := map[string]uint64{}
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return res
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		parts := strings.Fields(sc.Text())
		if len(parts) >= 2 {
			v, _ := strconv.ParseUint(parts[1], 10, 64)
			res[strings.TrimSuffix(parts[0], ":")] = v
		}
	}
	return res
}

func diskUsage() int {
	var st syscall.Statfs_t
	if err := syscall.Statfs("/", &st); err != nil {
		return 0
	}
	used := uint64(st.Blocks) - uint64(st.Bfree)
	denom := used + uint64(st.Bavail)
	if denom == 0 {
		return 0
	}
	return clamp(round(float64(used) / float64(denom) * 100))
}

func loadAvg() string {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return "0.0"
	}
	if f := strings.Fields(string(b)); len(f) > 0 {
		return f[0]
	}
	return "0.0"
}

func uptime() string {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return ""
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return ""
	}
	secs, _ := strconv.ParseFloat(f[0], 64)
	d := time.Duration(secs) * time.Second
	days := int(d.Hours()) / 24
	hrs := int(d.Hours()) % 24
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hrs)
	case hrs > 0:
		return fmt.Sprintf("%dh %dm", hrs, int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
}

func osName() string {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return runtime.GOOS
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := sc.Text(); strings.HasPrefix(line, "PRETTY_NAME=") {
			return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
		}
	}
	return runtime.GOOS
}

// --- GPU (NVIDIA via nvidia-smi) --------------------------------------------

func gpuStats() (util, vram *int, name, vramText string) {
	out, err := exec.Command("nvidia-smi",
		"--query-gpu=utilization.gpu,memory.used,memory.total,name",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return nil, nil, "", ""
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		return nil, nil, "", ""
	}
	parts := strings.Split(strings.Split(line, "\n")[0], ",")
	if len(parts) < 4 {
		return nil, nil, "", ""
	}
	u, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
	usedMB, _ := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	totMB, _ := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
	uu := clamp(u)
	vv := 0
	if totMB > 0 {
		vv = clamp(round(usedMB / totMB * 100))
	}
	return &uu, &vv, strings.TrimSpace(parts[3]),
		fmt.Sprintf("%.1f / %.0f GB", usedMB/1024, totMB/1024)
}

// --- assembly ---------------------------------------------------------------

func deriveStatus(cpu, mem, disk int, vram *int) string {
	switch {
	case disk >= 92 || mem >= 95:
		return "crit"
	case cpu >= 88 || mem >= 88 || disk >= 85 || (vram != nil && *vram >= 92):
		return "warn"
	default:
		return "good"
	}
}

// Collect takes a full reading of the current host.
func Collect() Snapshot {
	host, _ := os.Hostname()
	gpu, vram, name, vramText := gpuStats()
	cpu, mem, disk := cpuUsage(), memUsage(), diskUsage()
	rx, tx := netUsage()
	return Snapshot{
		Host:     host,
		Version:  version.Current(),
		OS:       osName(),
		Up:       uptime(),
		CPU:      cpu,
		Mem:      mem,
		Disk:     disk,
		GPU:      gpu,
		VRAM:     vram,
		GPUName:  name,
		VRAMText: vramText,
		Load:     loadAvg(),
		NetRx:    rx,
		NetTx:    tx,
		Status:   deriveStatus(cpu, mem, disk, vram),
	}
}

func clamp(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func round(f float64) int { return int(f + 0.5) }
