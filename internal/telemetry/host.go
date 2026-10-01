package telemetry

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Counters are the extra host readings stored with each sample. The network and
// disk values are cumulative since boot; the sampler turns them into rates.
type Counters struct {
	Load1, Load5, Load15 float64
	SwapUsed, SwapTotal  int64
	NetRx, NetTx         uint64 // bytes, physical interfaces only
	DiskRead, DiskWrite  uint64 // bytes, whole block devices only
}

// HostReader is the optional second half of Reader: a reader that cannot
// provide it simply leaves these columns at zero.
type HostReader interface {
	Counters() (Counters, error)
}

func (p ProcReader) sys() string {
	if p.SysRoot == "" {
		return "/sys"
	}
	return p.SysRoot
}

// Counters reads load average, swap, network and disk byte counters. A part
// that is unavailable (a container without /sys, a kernel without swap
// accounting) stays zero instead of failing the whole sample.
func (p ProcReader) Counters() (Counters, error) {
	var c Counters
	if b, err := os.ReadFile(filepath.Join(p.root(), "loadavg")); err == nil {
		f := strings.Fields(string(b))
		if len(f) >= 3 {
			c.Load1, _ = strconv.ParseFloat(f[0], 64)
			c.Load5, _ = strconv.ParseFloat(f[1], 64)
			c.Load15, _ = strconv.ParseFloat(f[2], 64)
		}
	}
	if mi, err := p.MemInfo(); err == nil {
		c.SwapTotal = mi.SwapTotal
		c.SwapUsed = max(mi.SwapTotal-mi.SwapFree, 0)
	}
	c.NetRx, c.NetTx = p.netBytes()
	c.DiskRead, c.DiskWrite = p.diskBytes()
	return c, nil
}

// MemInfo is the detail behind the single "used" percentage. Used follows the
// same definition as the sampled series: total minus available.
type MemInfo struct {
	Total, Available, Free, Buffers, Cached int64
	SwapTotal, SwapFree                     int64
}

func (p ProcReader) MemInfo() (MemInfo, error) {
	f, err := os.Open(filepath.Join(p.root(), "meminfo"))
	if err != nil {
		return MemInfo{}, err
	}
	defer f.Close()
	var m MemInfo
	var reclaim int64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		fs := strings.Fields(rest)
		if len(fs) == 0 {
			continue
		}
		kb, err := strconv.ParseInt(fs[0], 10, 64)
		if err != nil {
			continue
		}
		b := kb * 1024
		switch k {
		case "MemTotal":
			m.Total = b
		case "MemAvailable":
			m.Available = b
		case "MemFree":
			m.Free = b
		case "Buffers":
			m.Buffers = b
		case "Cached":
			m.Cached = b
		case "SReclaimable":
			reclaim = b
		case "SwapTotal":
			m.SwapTotal = b
		case "SwapFree":
			m.SwapFree = b
		}
	}
	m.Cached += reclaim
	return m, nil
}

// skippedNet are virtual interfaces whose traffic is already counted on the
// physical interface they ride on; including them would double the numbers.
var skippedNet = []string{"lo", "docker", "veth", "br-", "virbr", "cni", "flannel", "cali", "kube", "tap", "tun", "wg"}

func (p ProcReader) netBytes() (rx, tx uint64) {
	f, err := os.Open(filepath.Join(p.root(), "net", "dev"))
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		name, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		skip := false
		for _, s := range skippedNet {
			if name == s || (s != "lo" && strings.HasPrefix(name, s)) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 9 {
			continue
		}
		r, _ := strconv.ParseUint(f[0], 10, 64)
		t, _ := strconv.ParseUint(f[8], 10, 64)
		rx += r
		tx += t
	}
	return rx, tx
}

// diskBytes sums sectors (always 512 bytes in /proc/diskstats) over whole block
// devices: partitions would double count, and loop, ram and device-mapper
// volumes sit on top of devices that are already counted.
func (p ProcReader) diskBytes() (read, write uint64) {
	f, err := os.Open(filepath.Join(p.root(), "diskstats"))
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 10 {
			continue
		}
		name := f[2]
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") || strings.HasPrefix(name, "zram") ||
			strings.HasPrefix(name, "dm-") || strings.HasPrefix(name, "md") || strings.HasPrefix(name, "sr") {
			continue
		}
		// Whole devices have an entry in /sys/block; partitions live below them.
		if _, err := os.Stat(filepath.Join(p.sys(), "block", name)); err != nil {
			continue
		}
		r, _ := strconv.ParseUint(f[5], 10, 64)
		w, _ := strconv.ParseUint(f[9], 10, 64)
		read += r * 512
		write += w * 512
	}
	return read, write
}

// HostInfo is what stays the same while the machine runs.
type HostInfo struct {
	Hostname   string
	OS         string
	Kernel     string
	Arch       string
	CPUModel   string
	LogicalCPU int
	UptimeSec  int64
	BootedAtMS int64
}

// Info reads static host facts; anything unreadable is left empty.
func (p ProcReader) Info() HostInfo {
	h := HostInfo{Arch: runtime.GOARCH}
	h.Hostname, _ = os.Hostname()
	if b, err := os.ReadFile(filepath.Join(p.root(), "sys", "kernel", "osrelease")); err == nil {
		h.Kernel = strings.TrimSpace(string(b))
	}
	for _, path := range []string{"/etc/os-release", "/usr/lib/os-release"} {
		if b, err := os.ReadFile(path); err == nil {
			for _, line := range strings.Split(string(b), "\n") {
				if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
					h.OS = strings.Trim(v, `"'`)
				}
			}
			break
		}
	}
	if f, err := os.Open(filepath.Join(p.root(), "cpuinfo")); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if k, v, ok := strings.Cut(sc.Text(), ":"); ok && strings.TrimSpace(k) == "model name" {
				h.CPUModel = strings.TrimSpace(v)
				break
			}
		}
		f.Close()
	}
	h.LogicalCPU, _ = p.LogicalCPUs()
	if b, err := os.ReadFile(filepath.Join(p.root(), "uptime")); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			if up, err := strconv.ParseFloat(f[0], 64); err == nil {
				h.UptimeSec = int64(up)
				h.BootedAtMS = time.Now().Add(-time.Duration(up * float64(time.Second))).UnixMilli()
			}
		}
	}
	return h
}

// SelfInfo is the panel process.
type SelfInfo struct {
	PID        int
	RSSBytes   int64
	Threads    int
	OpenFiles  int
	FileLimit  int64
	Goroutines int
	HeapBytes  uint64 // live heap
	SysBytes   uint64 // memory obtained from the OS by the Go runtime
	GCCount    uint32
	GCPauseMS  float64 // total stop-the-world pause since start
	GoVersion  string
}

// Self reads the panel's own resource use.
func (p ProcReader) Self() SelfInfo {
	s := SelfInfo{PID: os.Getpid(), Goroutines: runtime.NumGoroutine(), GoVersion: runtime.Version()}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	s.HeapBytes, s.SysBytes, s.GCCount, s.GCPauseMS = m.HeapAlloc, m.Sys, m.NumGC, float64(m.PauseTotalNs)/1e6
	if f, err := os.Open(filepath.Join(p.root(), "self", "status")); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			k, v, ok := strings.Cut(sc.Text(), ":")
			if !ok {
				continue
			}
			fs := strings.Fields(v)
			if len(fs) == 0 {
				continue
			}
			switch k {
			case "VmRSS":
				if kb, err := strconv.ParseInt(fs[0], 10, 64); err == nil {
					s.RSSBytes = kb * 1024
				}
			case "Threads":
				s.Threads, _ = strconv.Atoi(fs[0])
			}
		}
		f.Close()
	}
	if ents, err := os.ReadDir(filepath.Join(p.root(), "self", "fd")); err == nil {
		s.OpenFiles = len(ents)
	}
	if f, err := os.Open(filepath.Join(p.root(), "self", "limits")); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if rest, ok := strings.CutPrefix(sc.Text(), "Max open files"); ok {
				if fs := strings.Fields(rest); len(fs) > 0 {
					s.FileLimit, _ = strconv.ParseInt(fs[0], 10, 64)
				}
			}
		}
		f.Close()
	}
	return s
}
