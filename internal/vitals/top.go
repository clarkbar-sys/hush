package vitals

// top.go collects the htop-style detail the fleet console shows when you open a
// machine's CPU/network panel: per-core utilisation and the busiest processes.
// Unlike the aggregate numbers in vitals.go (fed by the always-on 1s sampler),
// this is sampled on demand — two reads of /proc a short interval apart — so it
// costs nothing until someone is actually watching, and the CPU%% figures are a
// true rate rather than a single instantaneous reading.

import (
	"bufio"
	"os"
	"os/user"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Process is one running process as the console's process table understands it.
type Process struct {
	PID     int     `json:"pid"`
	User    string  `json:"user"`
	Command string  `json:"command"`
	CPU     float64 `json:"cpu"` // percent of one core, top-style (100 = a fully-busy core; can exceed it)
	Mem     float64 `json:"mem"` // resident set as a percent of physical RAM
	RSS     int64   `json:"rss"` // resident set size, bytes
}

// TopSnapshot is a single on-demand reading of a machine's live process/core
// state, served by the agent's /top endpoint.
type TopSnapshot struct {
	Host    string    `json:"host"`
	CPU     int       `json:"cpu"`     // overall CPU %, matching the fleet card's ring
	Mem     int       `json:"mem"`     // overall memory %, matching the fleet card's ring
	Cores   []int     `json:"cores"`   // per-core utilisation %, index == core number
	Procs   []Process `json:"procs"`   // busiest processes, already sorted by CPU desc
	Running int       `json:"running"` // total processes seen (Procs may be truncated)
}

// topSampleInterval is the gap between the two /proc reads a /top call takes.
// Long enough that per-process CPU deltas are meaningful, short enough that the
// endpoint still answers well inside the console's ~2s poll.
const topSampleInterval = 350 * time.Millisecond

// coreTimes is one CPU line's idle and total jiffies from /proc/stat.
type coreTimes struct{ idle, total uint64 }

// procSample is the per-process state read in one pass of /proc.
type procSample struct {
	comm     string
	jiffies  uint64 // utime+stime
	rssPages int64
	uid      uint32
}

// CollectTop takes an on-demand process/core reading, returning at most limit
// processes (the busiest by CPU). A limit <= 0 returns them all.
func CollectTop(limit int) TopSnapshot {
	host, _ := os.Hostname()

	cores1, agg1 := readPerCore()
	procs1 := readProcSamples()
	time.Sleep(topSampleInterval)
	cores2, agg2 := readPerCore()
	procs2 := readProcSamples()

	ncpu := len(cores2)
	if ncpu == 0 {
		ncpu = runtime.NumCPU()
	}

	cores := make([]int, 0, len(cores2))
	for i := range cores2 {
		if i < len(cores1) {
			cores = append(cores, corePct(cores1[i], cores2[i]))
		}
	}

	totalDelta := int64(agg2.total - agg1.total)
	memTotalBytes := int64(readMeminfo()["MemTotal"]) * 1024
	pageSize := int64(os.Getpagesize())

	procs := make([]Process, 0, len(procs2))
	for pid, p2 := range procs2 {
		p1, ok := procs1[pid]
		if !ok {
			continue // process appeared mid-sample; no delta to compute
		}
		delta := int64(p2.jiffies) - int64(p1.jiffies)
		if delta < 0 {
			delta = 0
		}
		cpu := 0.0
		if totalDelta > 0 {
			// delta/totalDelta is this process's share of all CPU capacity;
			// scaling by ncpu expresses it top-style, where 100 == one core.
			cpu = float64(delta) / float64(totalDelta) * 100 * float64(ncpu)
		}
		rss := p2.rssPages * pageSize
		mem := 0.0
		if memTotalBytes > 0 {
			mem = float64(rss) / float64(memTotalBytes) * 100
		}
		procs = append(procs, Process{
			PID:     pid,
			User:    userName(p2.uid),
			Command: p2.comm,
			CPU:     round1(cpu),
			Mem:     round1(mem),
			RSS:     rss,
		})
	}

	sort.Slice(procs, func(i, j int) bool {
		if procs[i].CPU != procs[j].CPU {
			return procs[i].CPU > procs[j].CPU
		}
		return procs[i].RSS > procs[j].RSS
	})

	running := len(procs)
	if limit > 0 && len(procs) > limit {
		procs = procs[:limit]
	}

	return TopSnapshot{
		Host:    host,
		CPU:     cpuUsage(),
		Mem:     memUsage(),
		Cores:   cores,
		Procs:   procs,
		Running: running,
	}
}

// readPerCore parses /proc/stat into per-core idle/total jiffies plus the
// aggregate "cpu" line. Cores are indexed by their number, so cores[3] is cpu3.
func readPerCore() (cores []coreTimes, agg coreTimes) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return nil, coreTimes{}
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 6 || !strings.HasPrefix(fields[0], "cpu") {
			continue
		}
		ct := sumCPUFields(fields[1:])
		if fields[0] == "cpu" {
			agg = ct
			continue
		}
		n, err := strconv.Atoi(fields[0][3:])
		if err != nil {
			continue
		}
		for len(cores) <= n {
			cores = append(cores, coreTimes{})
		}
		cores[n] = ct
	}
	return cores, agg
}

// sumCPUFields turns the numeric columns of one /proc/stat cpu line into idle
// (idle+iowait) and total jiffies, matching readCPUTimes in vitals.go.
func sumCPUFields(fields []string) coreTimes {
	var ct coreTimes
	for i, s := range fields {
		v, _ := strconv.ParseUint(s, 10, 64)
		ct.total += v
		if i == 3 || i == 4 { // idle + iowait
			ct.idle += v
		}
	}
	return ct
}

// corePct turns two readings of one core into a busy percentage.
func corePct(a, b coreTimes) int {
	totalD := int64(b.total - a.total)
	idleD := int64(b.idle - a.idle)
	if totalD <= 0 {
		return 0
	}
	return clamp(round((1 - float64(idleD)/float64(totalD)) * 100))
}

// readProcSamples walks /proc once, reading each process's CPU jiffies, command,
// resident pages and owning uid. Processes that vanish mid-walk are skipped.
func readProcSamples() map[int]procSample {
	res := map[int]procSample{}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return res
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue // not a process dir
		}
		data, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		comm, jiffies, rssPages, ok := parseStat(data)
		if !ok {
			continue
		}
		var uid uint32
		if fi, err := os.Stat("/proc/" + e.Name()); err == nil {
			if st, ok := fi.Sys().(*syscall.Stat_t); ok {
				uid = st.Uid
			}
		}
		res[pid] = procSample{comm: comm, jiffies: jiffies, rssPages: rssPages, uid: uid}
	}
	return res
}

// parseStat pulls the command, CPU jiffies (utime+stime) and resident pages out
// of one /proc/[pid]/stat line. The command sits in parentheses and may itself
// contain spaces or ')', so the fields after it are located from the *last* ')'
// — the standard way to parse this file safely.
func parseStat(data []byte) (comm string, jiffies uint64, rssPages int64, ok bool) {
	s := string(data)
	open := strings.IndexByte(s, '(')
	shut := strings.LastIndexByte(s, ')')
	if open < 0 || shut < 0 || shut < open {
		return "", 0, 0, false
	}
	comm = s[open+1 : shut]
	// Fields after the ')' begin at stat field 3 (state), so field N is at
	// index N-3 here: utime=14 -> 11, stime=15 -> 12, rss(pages)=24 -> 21.
	fields := strings.Fields(s[shut+1:])
	if len(fields) < 22 {
		return "", 0, 0, false
	}
	utime, _ := strconv.ParseUint(fields[11], 10, 64)
	stime, _ := strconv.ParseUint(fields[12], 10, 64)
	rss, _ := strconv.ParseInt(fields[21], 10, 64)
	return comm, utime + stime, rss, true
}

// userName resolves a uid to a login name, caching results (and falling back to
// the numeric uid) so a process table doesn't hammer /etc/passwd.
var (
	userMu    sync.Mutex
	userCache = map[uint32]string{}
)

func userName(uid uint32) string {
	userMu.Lock()
	defer userMu.Unlock()
	if n, ok := userCache[uid]; ok {
		return n
	}
	name := strconv.FormatUint(uint64(uid), 10)
	if u, err := user.LookupId(name); err == nil && u.Username != "" {
		name = u.Username
	}
	userCache[uid] = name
	return name
}

// round1 rounds to one decimal place, keeping the JSON CPU/mem figures tidy.
func round1(f float64) float64 {
	return float64(int(f*10+0.5)) / 10
}
