package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/jessevdk/go-flags"
	"github.com/kazeburo/mackerel-plugin-maxcpu/internal/statworker"
	"github.com/kazeburo/mackerel-plugin-maxcpu/maxcpu"
)

// version by Makefile
var version string

type cmdOpts struct {
	Socket   string `short:"s" long:"socket" required:"true" description:"Socket file used calcurating daemon" `
	AsDaemon bool   `long:"as-daemon" description:"run as daemon"`
	Version  bool   `short:"v" long:"version" description:"Show version"`
}

func CHello(c *maxcpu.Client) (*maxcpu.GetHelloResponse, error) {
	b, err := c.Get("hello")
	if err != nil {
		return nil, err
	}
	res := &maxcpu.GetHelloResponse{}
	err = json.Unmarshal(b, res)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func CStats(c *maxcpu.Client) (*maxcpu.GetStatsResponse, error) {
	b, err := c.Get("stats")
	if err != nil {
		return nil, err
	}
	res := &maxcpu.GetStatsResponse{}
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

func runBinaryCheck(socket string, current time.Time) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		modified, err := selfModified()
		if err == nil {
			if modified != current {
				cmd := exec.Command(os.Args[0], "--as-daemon", "--socket", socket)
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

func selfModified() (time.Time, error) {

	fs, err := os.Stat(os.Args[0])
	if err != nil {
		return time.Now(), err
	}
	return fs.ModTime(), nil
}

func execBackground(opts cmdOpts) int {
	// check proc before exec
	_, err := statworker.GetStat()
	if err != nil {
		log.Printf("%v", err)
		return 1
	}

	cmd := exec.Command(os.Args[0], "--as-daemon", "--socket", opts.Socket)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	err = cmd.Start()
	if err != nil {
		log.Printf("%v", err)
		return 1
	}
	time.After(5 * time.Second)
	return 0
}

var maxIdleTime int64 = 600

func runIdleCheck(w *statworker.Worker) {
	ticker := time.NewTicker(1 * time.Second)
	for range ticker.C {
		if w.IdleTime() > maxIdleTime {
			syscall.Kill(syscall.Getpid(), syscall.SIGKILL)
		}
	}
}

func runBackground(opts cmdOpts) int {

	modified, err := selfModified()
	if err != nil {
		log.Printf("%v", err)
		return 1
	}

	worker := statworker.New()

	go func() { worker.Run() }()
	go func() { runIdleCheck(worker) }()
	go func() { runBinaryCheck(opts.Socket, modified) }()

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

	unixListener, err := net.Listen("unix", opts.Socket)
	if err != nil {
		log.Printf("%v", err)
		return 1
	}

	cmd := exec.Command("ls", "-la")
	stdoutStderr, err := cmd.CombinedOutput()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s\n", stdoutStderr)

	mserver, _ := maxcpu.NewServer()
	mserver.Register("GET", worker.MGet)

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
