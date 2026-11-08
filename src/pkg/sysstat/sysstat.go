// Package sysstat reads what the Details box shows about the machine: CPU
// use, memory in use and the disk a folder is on.
package sysstat

import (
	"sync"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
)

// cpuWindow is how many readings the CPU figure averages: one per Sample,
// every 2 seconds, so the last 10 seconds.
const cpuWindow = 5

// Stats is one reading. A part that couldn't be read has its OK false.
type Stats struct {
	CPUOK      bool
	CPUPercent float64 // across all cores, averaged over the last readings
	Cores      int

	MemOK    bool
	MemUsed  uint64
	MemTotal uint64

	DiskOK    bool
	DiskUsed  uint64
	DiskTotal uint64
	DiskFree  uint64
}

// Sampler keeps the last CPU readings between samples.
type Sampler struct {
	mu   sync.Mutex
	cpus []float64
}

// Sample reads the machine now, with the disk that holds path.
func (s *Sampler) Sample(path string) Stats {
	var st Stats
	// With no interval, Percent compares with its previous call: the use
	// since the last Sample. The first call has nothing to compare with.
	if p, err := cpu.Percent(0, false); err == nil && len(p) == 1 {
		s.mu.Lock()
		s.cpus = append(s.cpus, p[0])
		if len(s.cpus) > cpuWindow {
			s.cpus = s.cpus[len(s.cpus)-cpuWindow:]
		}
		sum := 0.0
		for _, v := range s.cpus {
			sum += v
		}
		st.CPUPercent = sum / float64(len(s.cpus))
		s.mu.Unlock()
		st.CPUOK = true
	}
	if n, err := cpu.Counts(true); err == nil {
		st.Cores = n
	}
	if v, err := mem.VirtualMemory(); err == nil {
		st.MemOK, st.MemUsed, st.MemTotal = true, v.Used, v.Total
	}
	if path != "" {
		if u, err := disk.Usage(path); err == nil {
			st.DiskOK, st.DiskUsed, st.DiskTotal, st.DiskFree = true, u.Used, u.Total, u.Free
		}
	}
	return st
}
