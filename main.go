package main

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/jessevdk/go-flags"
	"github.com/prometheus/procfs"
)

// Version by Makefile
var Version string

type cmdOpts struct {
	Socket   string `short:"s" long:"socket" required:"true" description:"Socket file used calcurating daemon" `
	AsDaemon bool   `long:"as-daemon" description:"run as daemon"`
	Version  bool   `short:"v" long:"version" description:"Show version"`
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

var firstCPU cpuUsage
var cpuStats []*cpuUsage
var currentStat int64
var maxStats int64 = 361
var maxIdleTime int64 = 600
var idleTime int64 = 0
var statsLock sync.Mutex

func handleHello(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("OK\n"))
}

func execBackground(opts cmdOpts) int {
	cmd := exec.Command(os.Args[0], "--as-daemon", "--socket", opts.Socket)
	err := cmd.Start()
	if err != nil {
		log.Printf("%v", err)
		return 1
	}
	return 0
}

func cpuStat() (procfs.CPUStat, error) {
	// read /proc/stat
	cpu, err := procfs.NewStat()
	if err != nil {
		return procfs.CPUStat{}, err
	}
	return cpu.CPUTotal, nil
}

func runStats() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case _ = <-ticker.C:
			cpu, err := cpuStat()
			if err != nil {
				log.Printf("%v", err)
				continue
			}
			statsLock.Lock()
			idleTime++
			if idleTime > maxIdleTime {
				syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
			}
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

func round(f float64) int64 {
	return int64(math.Round(f)) - 1
}

func getStats(w http.ResponseWriter, r *http.Request) {
	statsLock.Lock()
	defer statsLock.Unlock()
	idleTime = 0
	var usages sort.Float64Slice
	var i int64
	var t float64
	for i = 1; i < maxStats; i++ {
		if cpuStats[i] != nil {
			usages = append(usages, cpuStats[i].Usage)
			t += cpuStats[i].Usage
		}
	}
	currentStat = 0
	cpuStats = make([]*cpuUsage, maxStats, maxStats)

	if len(usages) == 0 {
		w.WriteHeader(http.StatusTooEarly)
		w.Write([]byte("Calculating now\n"))
		return
	}

	now := time.Now().Unix()
	sort.Sort(usages)
	fl := float64(len(usages))

	buffer := ""
	buffer += fmt.Sprintf("maxcpu.us_sy_wa_si_st_usage.max\t%f\t%d\n", usages[round(fl)], now)
	buffer += fmt.Sprintf("maxcpu.us_sy_wa_si_st_usage.min\t%f\t%d\n", usages[0], now)
	buffer += fmt.Sprintf("maxcpu.us_sy_wa_si_st_usage.avg\t%f\t%d\n", t/fl, now)
	buffer += fmt.Sprintf("maxcpu.us_sy_wa_si_st_usage.90pt\t%f\t%d\n", usages[round(fl*0.90)], now)
	buffer += fmt.Sprintf("maxcpu.us_sy_wa_si_st_usage.75pt\t%f\t%d\n", usages[round(fl*0.75)], now)

	w.Write([]byte(buffer))
}

func runBackground(opts cmdOpts) int {
	statsLock.Lock()
	currentStat = 0
	cpuStats = make([]*cpuUsage, maxStats, maxStats)
	statsLock.Unlock()
	cpu, err := cpuStat()
	if err != nil {
		log.Printf("%v", err)
		return 1
	}
	go func() { runStats() }()
	defer os.Remove(opts.Socket)
	firstCPU = cpuUsage{
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
	time.Sleep(1 * time.Second)
	m := mux.NewRouter()
	m.HandleFunc("/", handleHello)
	m.HandleFunc("/hc", handleHello)
	m.HandleFunc("/get", getStats)

	server := http.Server{
		Handler: m,
	}

	idleConnsClosed := make(chan struct{})
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGTERM, os.Interrupt)
		<-sigChan
		if es := server.Shutdown(context.Background()); es != nil {
			log.Printf("Shutdown error: %s", es)
		}
		close(idleConnsClosed)
	}()

	unixListener, err := net.Listen("unix", opts.Socket)
	if err != nil {
		log.Printf("%v", err)
		return 1
	}
	if err := server.Serve(unixListener); err != http.ErrServerClosed {
		log.Printf("%v", err)
		return 1
	}
	<-idleConnsClosed
	return 0
}

func makeTransport(opts cmdOpts) http.Client {
	client := http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.DialTimeout("unix", opts.Socket, 1*time.Second)
			},
			ResponseHeaderTimeout: 1 * time.Second,
		},
	}
	return client
}

func checkHTTPalive(opts cmdOpts) bool {
	client := makeTransport(opts)
	res, err := client.Get("http://unix/hc")
	if err != nil {
		log.Printf("%v", err)
		return false
	}
	io.Copy(ioutil.Discard, res.Body)
	res.Body.Close()
	return true
}

func getHTTPstats(opts cmdOpts) int {
	client := makeTransport(opts)
	res, err := client.Get("http://unix/get")
	if err != nil {
		log.Printf("%v", err)
		return 1
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusOK {
		io.Copy(os.Stdout, res.Body)
		return 0
	}
	buf, _ := ioutil.ReadAll(res.Body)
	log.Printf("%s", string(buf))

	return 1
}

func main() {
	os.Exit(_main())
}

func _main() int {
	opts := cmdOpts{}
	psr := flags.NewParser(&opts, flags.Default)
	_, err := psr.Parse()
	if err != nil {
		return 1
	}
	if opts.Version {
		fmt.Printf(`%s %s
Compiler: %s %s
`,
			os.Args[0],
			Version,
			runtime.Compiler,
			runtime.Version())
		return 0
	}

	if err != nil {
		log.Printf("%v", err)
		return 1
	}

	if opts.AsDaemon {
		return runBackground(opts)
	}

	if !checkHTTPalive(opts) {
		// exec daemon
		log.Printf("start background process")
		return execBackground(opts)

	}

	return getHTTPstats(opts)
}
