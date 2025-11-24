package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	connect "github.com/bufbuild/connect-go"
	"github.com/jessevdk/go-flags"
	"github.com/kazeburo/mackerel-plugin-maxcpu/internal/statworker"
	"github.com/kazeburo/mackerel-plugin-maxcpu/maxcpu"
	maxcpuconnect "github.com/kazeburo/mackerel-plugin-maxcpu/maxcpu/maxcpuconnect"
	"google.golang.org/protobuf/types/known/emptypb"
)

// version by Makefile
var version string

type Opt struct {
	Socket   string `short:"s" long:"socket" required:"true" description:"Socket file used calcurating daemon" `
	AsDaemon bool   `long:"as-daemon" description:"run as daemon"`
	Version  bool   `short:"v" long:"version" description:"Show version"`
	client   maxcpuconnect.MaxCPUClient
}

// connect-go用のMaxCPUHandlerアダプタ
type workerConnectHandler struct {
	worker *statworker.Worker
}

func (h *workerConnectHandler) GetStats(ctx context.Context, req *connect.Request[emptypb.Empty]) (*connect.Response[maxcpu.StatsResponse], error) {
	resp, err := h.worker.GetStats(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(resp), nil
}

func (h *workerConnectHandler) Hello(ctx context.Context, req *connect.Request[emptypb.Empty]) (*connect.Response[maxcpu.HelloResponse], error) {
	resp, err := h.worker.Hello(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(resp), nil
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

	mux := http.NewServeMux()
	connectHandler := &workerConnectHandler{worker: worker}
	path, handler := maxcpuconnect.NewMaxCPUHandler(connectHandler)
	mux.Handle(path, handler)

	idleConnsClosed := make(chan struct{})
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGTERM, os.Interrupt)
		<-sigChan
		close(idleConnsClosed)
	}()

	os.Remove(opt.Socket)
	unixListener, err := net.Listen("unix", opt.Socket)
	if err != nil {
		log.Printf("%v", err)
		return 1
	}
	srv := &http.Server{Handler: mux}
	go func() {
		if err := srv.Serve(unixListener); err != nil && err != http.ErrServerClosed {
			log.Printf("%v", err)
		}
	}()
	<-idleConnsClosed
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	return 0
}

func checkDaemonAlive(opt *Opt) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	req := connect.NewRequest(&emptypb.Empty{})
	_, err := opt.client.Hello(ctx, req)
	if err != nil {
		log.Printf("check daemon alive failed: %v", err)
		return false
	}
	return true
}

func getStats(opt *Opt) int {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	res, err := opt.client.GetStats(ctx, connect.NewRequest(&emptypb.Empty{}))
	if err != nil {
		log.Printf("%v", err)
		return 1
	}
	for _, m := range res.Msg.Metrics {
		fmt.Printf(
			"maxcpu.us_sy_wa_si_st_usage.%s\t%f\t%d\n",
			m.Key,
			m.Metric,
			m.Epoch,
		)
	}
	return 0
}

func makeClient(socket string) (maxcpuconnect.MaxCPUClient, error) {
	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return net.DialTimeout("unix", socket, 1*time.Second)
			},
		},
	}
	baseURL := "http://unix"
	c := maxcpuconnect.NewMaxCPUClient(httpClient, baseURL)
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
