package statworker

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strconv"
)

type cpuStat struct {
	User      float64
	Nice      float64
	System    float64
	Idle      float64
	Iowait    float64
	IRQ       float64
	SoftIRQ   float64
	Steal     float64
	Guest     float64
	GuestNice float64
}

// cpuLineHeader is the prefix for the CPU line in /proc/stat
var cpuLineHeader = []byte("cpu ")

// https://github.com/prometheus/procfs/blob/c0c2a8be4d30a2e2cb95ea371a6f32a506d3e45e/proc_stat.go#L40
var userHZ float64 = 100

func parseCPUstat(b []byte) (float64, error) {
	f, err := strconv.ParseFloat(string(b), 64)
	if err != nil {
		return f, err
	}
	return f / userHZ, nil
}

func GetStat() (*cpuStat, error) {
	// read /proc/stat
	f, err := os.Open("/proc/stat")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Get the CPU statistics from /proc/stat
	return getCPUStat(f)
}

// cpu  168487 7399 36999 7766545 3915 0 13480 0 0 0
// qw(cpu-user cpu-nice cpu-system cpu-idle cpu-iowait cpu-irq cpu-softirq cpu-steal cpu-guest cpu-guest-nice);
func getCPUStat(f *os.File) (*cpuStat, error) {
	s := bufio.NewScanner(f)
	for s.Scan() {
		l := s.Bytes()
		if bytes.HasPrefix(l, cpuLineHeader) {
			fix := 0
			if l[len(cpuLineHeader)+1] == ' ' {
				fix = 1
			}
			cs := &cpuStat{}
			sp := bytes.Split(l[len(cpuLineHeader)+fix+1:], []byte(" "))
			if len(sp) > 0 {
				f, err := parseCPUstat(sp[0])
				if err != nil {
					return nil, err
				}
				cs.User = f
			}
			if len(sp) > 1 {
				f, err := parseCPUstat(sp[1])
				if err != nil {
					return nil, err
				}
				cs.Nice = f
			}
			if len(sp) > 2 {
				f, err := parseCPUstat(sp[2])
				if err != nil {
					return nil, err
				}
				cs.System = f
			}
			if len(sp) > 3 {
				f, err := parseCPUstat(sp[3])
				if err != nil {
					return nil, err
				}
				cs.Idle = f
			}
			if len(sp) > 4 {
				f, err := parseCPUstat(sp[4])
				if err != nil {
					return nil, err
				}
				cs.Iowait = f
			}
			if len(sp) > 5 {
				f, err := parseCPUstat(sp[5])
				if err != nil {
					return nil, err
				}
				cs.IRQ = f
			}
			if len(sp) > 6 {
				f, err := parseCPUstat(sp[6])
				if err != nil {
					return nil, err
				}
				cs.SoftIRQ = f
			}
			if len(sp) > 7 {
				f, err := parseCPUstat(sp[7])
				if err != nil {
					return nil, err
				}
				cs.Steal = f
			}
			if len(sp) > 8 {
				f, err := parseCPUstat(sp[8])
				if err != nil {
					return nil, err
				}
				cs.Guest = f
			}
			if len(sp) > 9 {
				f, err := parseCPUstat(sp[9])
				if err != nil {
					return nil, err
				}
				cs.GuestNice = f
			}
			return cs, nil
		}
	}
	if err := s.Err(); err != nil {
		return nil, fmt.Errorf("scanner error: %w", err)
	}
	return nil, fmt.Errorf("no cpu stats found in /proc/stat")
}
