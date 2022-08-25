package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/jessevdk/go-flags"
	"github.com/kazeburo/mackerel-plugin-maxcpu/maxcpu"
	reuse "github.com/libp2p/go-reuseport"
)

// version by Makefile
var version string

type cmdOpts struct {
	Socket   string `short:"s" long:"socket" required:"true" description:"Socket file used calcurating daemon" `
	AsDaemon bool   `long:"as-daemon" description:"run as daemon"`
	Version  bool   `short:"v" long:"version" description:"Show version"`
}

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

type getHelloResponse struct {
	Message string `json:"message"`
}

type getStatsResponse struct {
	Error   string    `json:"error"`
	Metrics []*Metric `json:"metrics"`
}

type Metric struct {
	Key    string  `json:"key"`
	Metric float64 `json:"metric"`
	Epoch  int64   `json:"epoch"`
}

var cpuStats []*cpuUsage
var currentStat int64
var maxStats int64 = 361
var maxIdleTime int64 = 600
var idleTime int64 = 0
var statsLock sync.Mutex

func round(f float64) int64 {
	return int64(math.Round(f)) - 1
}

func CHello(c *maxcpu.Client) (*getHelloResponse, error) {
	b, err := c.Get("hello")
	if err != nil {
		return nil, err
	}
	res := &getHelloResponse{}
	err = json.Unmarshal(b, res)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func CStats(c *maxcpu.Client) (*getStatsResponse, error) {
	b, err := c.Get("stats")
	if err != nil {
		return nil, err
	}
	res := &getStatsResponse{}
	err = json.Unmarshal(b, res)
	if err != nil {
		return nil, err
	}
	if res.Error != "" {
		return nil, fmt.Errorf(res.Error)
	}
	if len(res.Metrics) == 0 {
		return nil, fmt.Errorf("could not fetch any metrics")
	}
	return res, nil
}

func MGet(keys []string) (*maxcpu.Response, error) {
	if len(keys) == 0 || len(keys) > 1 {
		return nil, fmt.Errorf("no arguments or too many arguments for get")
	}
	switch keys[0] {
	case "hello":
		return MHello(keys[0])
	case "stats":
		return MStats(keys[0])
	default:
		return maxcpu.NotFound, nil
	}
}

func MHello(key string) (*maxcpu.Response, error) {
	res := getHelloResponse{"OK"}
	b, _ := json.Marshal(res)
	mres := &maxcpu.Response{}
	mres.Values = append(mres.Values, &maxcpu.Value{
		Key:  key,
		Data: b,
	})
	return mres, nil
}

func MStats(key string) (*maxcpu.Response, error) {
	// update idle time
	atomic.StoreInt64(&idleTime, 0)

	statsLock.Lock()
	defer statsLock.Unlock()
	var usages sort.Float64Slice
	var i int64
	var total float64
	for i = 1; i < maxStats; i++ {
		if cpuStats[i] != nil {
			usages = append(usages, cpuStats[i].Usage)
			total += cpuStats[i].Usage
		}
	}

	// clear stats
	current := cpuStats[currentStat]
	currentStat = 0
	cpuStats = make([]*cpuUsage, maxStats)
	cpuStats[0] = current

	var res getStatsResponse

	if len(usages) < 2 {
		res.Error = "Calculating now"
		b, _ := json.Marshal(res)
		mres := &maxcpu.Response{}
		mres.Values = append(mres.Values, &maxcpu.Value{
			Key:  key,
			Data: b,
		})
		return mres, nil
	}

	sort.Sort(usages)
	flen := float64(len(usages))
	epoch := time.Now().Unix()

	res.Metrics = append(res.Metrics, &Metric{
		Key:    "max",
		Metric: usages[round(flen)],
		Epoch:  epoch,
	})
	res.Metrics = append(res.Metrics, &Metric{
		Key:    "min",
		Metric: usages[0],
		Epoch:  epoch,
	})
	res.Metrics = append(res.Metrics, &Metric{
		Key:    "avg",
		Metric: total / flen,
		Epoch:  epoch,
	})
	res.Metrics = append(res.Metrics, &Metric{
		Key:    "90pt",
		Metric: usages[round(flen*0.90)],
		Epoch:  epoch,
	})
	res.Metrics = append(res.Metrics, &Metric{
		Key:    "75pt",
		Metric: usages[round(flen*0.75)],
		Epoch:  epoch,
	})

	b, _ := json.Marshal(res)
	mres := &maxcpu.Response{}
	mres.Values = append(mres.Values, &maxcpu.Value{
		Key:  key,
		Data: b,
	})

	return mres, nil
}

var cpuLineHeader = []byte("cpu ")

// https://github.com/prometheus/procfs/blob/c0c2a8be4d30a2e2cb95ea371a6f32a506d3e45e/proc_stat.go#L40
var userHZ float64 = 100

func parseCPUstat(b []byte) (float64, error) {
	f, err := strconv.ParseFloat(*(*string)(unsafe.Pointer(&b)), 64)
	if err != nil {
		return f, err
	}
	return f / userHZ, nil
}

// cpu  168487 7399 36999 7766545 3915 0 13480 0 0 0
// qw(cpu-user cpu-nice cpu-system cpu-idle cpu-iowait cpu-irq cpu-softirq cpu-steal cpu-guest cpu-guest-nice);
func getCPUStat() (*cpuStat, error) {
	// read /proc/stat
	f, err := os.Open("/proc/stat")
	if err != nil {
		return nil, err
	}
	defer f.Close()
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
	return nil, fmt.Errorf("no cpu stats found in /proc/stat")
}

func runBinaryCheck(opts cmdOpts, current time.Time) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			modified, err := selfModified()
			if err == nil {
				if modified != current {
					cmd := exec.Command(os.Args[0], "--as-daemon", "--socket", opts.Socket)
					err = cmd.Start()
					if err != nil {
						log.Printf("%v", err)
					} else {
						time.Sleep(10 * time.Second)
						// sockファイルを消さないようsigkillで止める
						syscall.Kill(syscall.Getpid(), syscall.SIGKILL)
					}
				}
			}
		}
	}
}

func runIdleCheck() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			atomic.AddInt64(&idleTime, 1)
			if atomic.LoadInt64(&idleTime) > maxIdleTime {
				// sockファイルを消さないようsigkillで止める
				syscall.Kill(syscall.Getpid(), syscall.SIGKILL)
			}
		}
	}
}

func runStats() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			cpu, err := getCPUStat()
			if err != nil {
				log.Printf("%v", err)
				continue
			}
			statsLock.Lock()
			if cpuStats[0] == nil {
				// first time
				cpuStats[0] = &cpuUsage{
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
			} else {
				next := currentStat + 1
				if next >= maxStats {
					next = 1
				}
				cpuStats[next] = &cpuUsage{
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
					GapUser:      cpu.User - cpuStats[currentStat].User,
					GapNice:      cpu.Nice - cpuStats[currentStat].Nice,
					GapSystem:    cpu.System - cpuStats[currentStat].System,
					GapIdle:      cpu.Idle - cpuStats[currentStat].Idle,
					GapIowait:    cpu.Iowait - cpuStats[currentStat].Iowait,
					GapIRQ:       cpu.IRQ - cpuStats[currentStat].IRQ,
					GapSoftIRQ:   cpu.SoftIRQ - cpuStats[currentStat].SoftIRQ,
					GapSteal:     cpu.Steal - cpuStats[currentStat].Steal,
					GapGuest:     cpu.Guest - cpuStats[currentStat].Guest,
					GapGuestNice: cpu.GuestNice - cpuStats[currentStat].GuestNice,
				}
				cpuStats[next].Usage = ((cpuStats[next].GapUser +
					cpuStats[next].GapSystem +
					cpuStats[next].GapIowait +
					cpuStats[next].GapSoftIRQ +
					cpuStats[next].GapSteal) /
					(cpuStats[next].GapUser +
						cpuStats[next].GapNice +
						cpuStats[next].GapSystem +
						cpuStats[next].GapIdle +
						cpuStats[next].GapIowait +
						cpuStats[next].GapIRQ +
						cpuStats[next].GapSoftIRQ +
						cpuStats[next].GapSteal +
						cpuStats[next].GapGuest +
						cpuStats[next].GapGuestNice)) * 100.0
				currentStat = next
			}
			statsLock.Unlock()
		}
	}
}

func selfModified() (time.Time, error) {

	fs, err := os.Stat(os.Args[0])
	if err != nil {
		return time.Now(), err
	}
	return fs.ModTime(), nil
}

func execBackground(opts cmdOpts) int {
	// check proc before exec
	_, err := getCPUStat()
	if err != nil {
		log.Printf("%v", err)
		return 1
	}

	cmd := exec.Command(os.Args[0], "--as-daemon", "--socket", opts.Socket)
	err = cmd.Start()
	if err != nil {
		log.Printf("%v", err)
		return 1
	}
	return 0
}

func runBackground(opts cmdOpts) int {
	statsLock.Lock()
	// initilize stats
	currentStat = 0
	cpuStats = make([]*cpuUsage, maxStats)
	statsLock.Unlock()

	modified, err := selfModified()
	if err != nil {
		log.Printf("%v", err)
		return 1
	}

	go func() { runStats() }()
	go func() { runIdleCheck() }()
	go func() { runBinaryCheck(opts, modified) }()

	time.Sleep(1 * time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	idleConnsClosed := make(chan struct{})
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGTERM, os.Interrupt)
		<-sigChan
		cancel()
		close(idleConnsClosed)
	}()

	os.Remove(opts.Socket)

	unixListener, err := reuse.Listen("unix", opts.Socket)
	if err != nil {
		log.Printf("%v", err)
		return 1
	}

	mserver, _ := maxcpu.NewServer()
	mserver.Register("GET", MGet)

	if err := mserver.Start(ctx, unixListener); err != nil {
		log.Printf("%v", err)
		return 1
	}
	<-idleConnsClosed
	return 0
}

func makeClient(opts cmdOpts) (*maxcpu.Client, error) {
	dialer := func() (net.Conn, error) {
		return net.DialTimeout("unix", opts.Socket, 5*time.Second)
	}
	c, err := maxcpu.NewClient(dialer)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func checkDaemonAlive(opts cmdOpts) bool {
	client, err := makeClient(opts)
	if err != nil {
		log.Printf("%v", err)
		return false
	}
	_, err = CHello(client)
	if err != nil {
		log.Printf("%v", err)
		return false
	}
	return true
}

func getStats(opts cmdOpts) int {
	client, err := makeClient(opts)
	if err != nil {
		log.Printf("%v", err)
		return 1
	}

	res, err := CStats(client)
	if err != nil {
		log.Printf("%v", err)
		return 1
	}
	for _, m := range res.Metrics {
		fmt.Printf(
			"maxcpu.us_sy_wa_si_st_usage.%s\t%f\t%d\n",
			m.Key,
			m.Metric,
			m.Epoch,
		)
	}
	return 1
}

func main() {
	os.Exit(_main())
}

func _main() int {
	opts := cmdOpts{}
	psr := flags.NewParser(&opts, flags.HelpFlag|flags.PassDoubleDash)
	_, err := psr.Parse()
	if opts.Version {
		fmt.Printf(`%s %s
Compiler: %s %s
`,
			os.Args[0],
			version,
			runtime.Compiler,
			runtime.Version())
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}

	if opts.AsDaemon {
		return runBackground(opts)
	}

	if !checkDaemonAlive(opts) {
		// exec daemon
		log.Printf("start background process")
		return execBackground(opts)
	}

	return getStats(opts)
}
