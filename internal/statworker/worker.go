package statworker

import (
	"log"
	"sync"
	"sync/atomic"
	"time"
)

type Worker struct {
	usages   []*cpuUsage
	current  int64
	lock     sync.Mutex
	idleTime int64
}

type cpuUsage struct {
	User         float64
	Nice         float64
	System       float64
	Idle         float64
	Iowait       float64
	IRQ          float64
	SoftIRQ      float64
	Steal        float64
	Guest        float64
	GuestNice    float64
	GapUser      float64
	GapNice      float64
	GapSystem    float64
	GapIdle      float64
	GapIowait    float64
	GapIRQ       float64
	GapSoftIRQ   float64
	GapSteal     float64
	GapGuest     float64
	GapGuestNice float64
	Usage        float64
}

// historySize defines the maximum number of CPU usage records to retain.
// The value 361 was chosen to represent a specific time period (e.g., 6 minutes and 1 second)
// or based on hardware constraints. Adjust as needed for your application.
const historySize = 361

func New() *Worker {
	usages := make([]*cpuUsage, historySize)
	return &Worker{
		usages:   usages,
		current:  0,
		idleTime: 0,
	}
}

func (w *Worker) calculatingGap(cpu *cpuStat) {
	w.lock.Lock()
	defer w.lock.Unlock()
	if w.usages[0] == nil {
		// first time
		w.usages[0] = &cpuUsage{
			User:      cpu.User,
			Nice:      cpu.Nice,
			System:    cpu.System,
			Idle:      cpu.Idle,
			Iowait:    cpu.Iowait,
			IRQ:       cpu.IRQ,
			SoftIRQ:   cpu.SoftIRQ,
			Steal:     cpu.Steal,
			Guest:     cpu.Guest,
			GuestNice: cpu.GuestNice,
		}
		return
	}
	next := w.current + 1
	if next >= historySize {
		next = 1
	}
	w.usages[next] = &cpuUsage{
		User:         cpu.User,
		Nice:         cpu.Nice,
		System:       cpu.System,
		Idle:         cpu.Idle,
		Iowait:       cpu.Iowait,
		IRQ:          cpu.IRQ,
		SoftIRQ:      cpu.SoftIRQ,
		Steal:        cpu.Steal,
		Guest:        cpu.Guest,
		GuestNice:    cpu.GuestNice,
		GapUser:      cpu.User - w.usages[w.current].User,
		GapNice:      cpu.Nice - w.usages[w.current].Nice,
		GapSystem:    cpu.System - w.usages[w.current].System,
		GapIdle:      cpu.Idle - w.usages[w.current].Idle,
		GapIowait:    cpu.Iowait - w.usages[w.current].Iowait,
		GapIRQ:       cpu.IRQ - w.usages[w.current].IRQ,
		GapSoftIRQ:   cpu.SoftIRQ - w.usages[w.current].SoftIRQ,
		GapSteal:     cpu.Steal - w.usages[w.current].Steal,
		GapGuest:     cpu.Guest - w.usages[w.current].Guest,
		GapGuestNice: cpu.GuestNice - w.usages[w.current].GuestNice,
	}
	w.usages[next].Usage = ((w.usages[next].GapUser +
		w.usages[next].GapSystem +
		w.usages[next].GapIowait +
		w.usages[next].GapSoftIRQ +
		w.usages[next].GapSteal) /
		(w.usages[next].GapUser +
			w.usages[next].GapNice +
			w.usages[next].GapSystem +
			w.usages[next].GapIdle +
			w.usages[next].GapIowait +
			w.usages[next].GapIRQ +
			w.usages[next].GapSoftIRQ +
			w.usages[next].GapSteal +
			w.usages[next].GapGuest +
			w.usages[next].GapGuestNice)) * 100.0
	w.current = next
}

func (w *Worker) Run() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		// increment idle time
		atomic.AddInt64(&w.idleTime, 1)

		cpu, err := GetStat()
		if err != nil {
			log.Printf("%v", err)
			continue
		}
		w.calculatingGap(cpu)
	}
}
