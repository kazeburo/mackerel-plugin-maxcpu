package main

import (
	"context"
	"crypto/md5"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/jessevdk/go-flags"
	"github.com/kazeburo/mackerel-plugin-maxcpu/maxcpu"
	reuse "github.com/libp2p/go-reuseport"
	"github.com/prometheus/procfs"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
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

var cpuStats []*cpuUsage
var currentStat int64
var maxStats int64 = 361
var maxIdleTime int64 = 600
var idleTime int64 = 0
var statsLock sync.Mutex

func round(f float64) int64 {
	return int64(math.Round(f)) - 1
}

type MaxCPUServer struct{}

func (*MaxCPUServer) Hello(_ context.Context, _ *empty.Empty) (*maxcpu.HelloResponse, error) {
	return &maxcpu.HelloResponse{Message: "OK"}, nil
}

func (*MaxCPUServer) GetStats(_ context.Context, _ *empty.Empty) (*maxcpu.StatsResponse, error) {

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
	cpuStats = make([]*cpuUsage, maxStats, maxStats)
	cpuStats[0] = current

	if len(usages) < 2 {
		return nil, status.Errorf(codes.Unavailable, "Calculating now")
	}

	sort.Sort(usages)
	flen := float64(len(usages))
	epoch := time.Now().Unix()

	metrics := []*maxcpu.Metric{}

	metrics = append(metrics, &maxcpu.Metric{
		Key:    "max",
		Metric: usages[round(flen)],
		Epoch:  epoch,
	})
	metrics = append(metrics, &maxcpu.Metric{
		Key:    "min",
		Metric: usages[0],
		Epoch:  epoch,
	})
	metrics = append(metrics, &maxcpu.Metric{
		Key:    "avg",
		Metric: total / flen,
		Epoch:  epoch,
	})
	metrics = append(metrics, &maxcpu.Metric{
		Key:    "90pt",
		Metric: usages[round(flen*0.90)],
		Epoch:  epoch,
	})
	metrics = append(metrics, &maxcpu.Metric{
		Key:    "75pt",
		Metric: usages[round(flen*0.75)],
		Epoch:  epoch,
	})

	return &maxcpu.StatsResponse{Metrics: metrics}, nil
}

func cpuStat() (procfs.CPUStat, error) {
	// read /proc/stat
	cpu, err := procfs.NewStat()
	if err != nil {
		return procfs.CPUStat{}, err
	}
	return cpu.CPUTotal, nil
}

func runBinaryCheck(opts cmdOpts, current string) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case _ = <-ticker.C:
			md5, err := selfCheckSum()
			if err == nil {
				if md5 != current {
					cmd := exec.Command(os.Args[0], "--as-daemon", "--socket", opts.Socket)
					err = cmd.Start()
					if err != nil {
						log.Printf("%v", err)
					} else {
						syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
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
		case _ = <-ticker.C:
			atomic.AddInt64(&idleTime, 1)
			if atomic.LoadInt64(&idleTime) > maxIdleTime {
				syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
			}
		}
	}
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

func selfCheckSum() (string, error) {
	b, err := ioutil.ReadFile(os.Args[0])
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", md5.Sum(b)), nil
}

func execBackground(opts cmdOpts) int {
	// check proc before exec
	_, err := cpuStat()
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
	cpuStats = make([]*cpuUsage, maxStats, maxStats)
	statsLock.Unlock()

	md5, err := selfCheckSum()
	if err != nil {
		log.Printf("%v", err)
		return 1
	}

	go func() { runStats() }()
	go func() { runIdleCheck() }()
	go func() { runBinaryCheck(opts, md5) }()

	time.Sleep(1 * time.Second)

	defer os.Remove(opts.Socket)

	server := grpc.NewServer()
	maxcpu.RegisterMaxCPUServer(server, &MaxCPUServer{})

	idleConnsClosed := make(chan struct{})
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGTERM, os.Interrupt)
		<-sigChan
		server.GracefulStop()
		close(idleConnsClosed)
	}()

	unixListener, err := reuse.Listen("unix", opts.Socket)
	if err != nil {
		log.Printf("%v", err)
		return 1
	}
	if err := server.Serve(unixListener); err != nil {
		log.Printf("%v", err)
		return 1
	}
	<-idleConnsClosed
	return 0
}

func makeTransport(opts cmdOpts) (maxcpu.MaxCPUClient, error) {
	dialer := func(a string, t time.Duration) (net.Conn, error) {
		return net.DialTimeout("unix", a, 1*time.Second)
	}
	conn, err := grpc.Dial(opts.Socket, grpc.WithInsecure(), grpc.WithDialer(dialer))
	if err != nil {
		return nil, err
	}
	c := maxcpu.NewMaxCPUClient(conn)
	return c, nil
}

func checkDaemonAlive(opts cmdOpts) bool {
	client, err := makeTransport(opts)
	if err != nil {
		log.Printf("%v", err)
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_, err = client.Hello(ctx, &emptypb.Empty{})
	if err != nil {
		log.Printf("%v", err)
		return false
	}
	return true
}

func getStats(opts cmdOpts) int {
	client, err := makeTransport(opts)
	if err != nil {
		log.Printf("%v", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	res, err := client.GetStats(ctx, &emptypb.Empty{})
	if err != nil {
		log.Printf("%v", err)
		return 1
	}
	for _, m := range res.GetMetrics() {
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

	if !checkDaemonAlive(opts) {
		// exec daemon
		log.Printf("start background process")
		return execBackground(opts)

	}

	return getStats(opts)
}
