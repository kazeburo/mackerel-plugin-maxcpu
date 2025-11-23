package main

import (
	"context"
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
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
)

// version by Makefile
var version string

type Opt struct {
	Socket   string `short:"s" long:"socket" required:"true" description:"Socket file used calcurating daemon" `
	AsDaemon bool   `long:"as-daemon" description:"run as daemon"`
	Version  bool   `short:"v" long:"version" description:"Show version"`
	client   maxcpu.MaxCPUClient
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

func execBackground(opt *Opt) int {
	// check proc before exec
	_, err := statworker.GetStat()
	if err != nil {
		log.Printf("%v", err)
		return 1
	}

	cmd := exec.Command(os.Args[0], "--as-daemon", "--socket", opt.Socket)
	err = cmd.Start()
	if err != nil {
		log.Printf("%v", err)
		return 1
	}
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

func runBackground(opt *Opt) int {

	modified, err := selfModified()
	if err != nil {
		log.Printf("%v", err)
		return 1
	}

	worker := statworker.New()

	go func() { worker.Run() }()
	go func() { runIdleCheck(worker) }()
	go func() { runBinaryCheck(opt.Socket, modified) }()

	time.Sleep(1 * time.Second)

	server := grpc.NewServer()
	maxcpu.RegisterMaxCPUServer(server, worker)

	idleConnsClosed := make(chan struct{})
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGTERM, os.Interrupt)
		<-sigChan
		server.GracefulStop()
		close(idleConnsClosed)
	}()

	os.Remove(opt.Socket)

	unixListener, err := net.Listen("unix", opt.Socket)
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

func checkDaemonAlive(opt *Opt) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_, err := opt.client.Hello(ctx, &emptypb.Empty{})
	if err != nil {
		log.Printf("check daemon alive failed: %v", err)
		return false
	}
	return true
}

func getStats(opt *Opt) int {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	res, err := opt.client.GetStats(ctx, &emptypb.Empty{})
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
	return 0
}

func makeClient(socket string) (maxcpu.MaxCPUClient, error) {
	dialer := func(_ context.Context, _ string) (net.Conn, error) {
		return net.DialTimeout("unix", socket, 1*time.Second)
	}
	conn, err := grpc.NewClient(
		"unix:"+socket,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(dialer),
	)
	if err != nil {
		return nil, err
	}
	c := maxcpu.NewMaxCPUClient(conn)
	return c, nil
}

func main() {
	os.Exit(_main())
}

func _main() int {
	opt := &Opt{}
	psr := flags.NewParser(opt, flags.HelpFlag|flags.PassDoubleDash)
	_, err := psr.Parse()
	if opt.Version {
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

	if opt.AsDaemon {
		return runBackground(opt)
	}

	client, err := makeClient(opt.Socket)
	if err != nil {
		log.Printf("%v", err)
		return 1
	}
	opt.client = client

	if !checkDaemonAlive(opt) {
		// exec daemon
		log.Printf("start background process")
		return execBackground(opt)
	}

	return getStats(opt)
}
