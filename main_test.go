package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"path"
	"testing"
	"time"

	connect "github.com/bufbuild/connect-go"
	"github.com/kazeburo/mackerel-plugin-maxcpu/internal/statworker"
	maxcpuconnect "github.com/kazeburo/mackerel-plugin-maxcpu/maxcpu/maxcpuconnect"
	"google.golang.org/protobuf/types/known/emptypb"
)

func startTestConnectServer(socket string) (*http.Server, func()) {
	_ = os.Remove(socket)
	worker := statworker.New()
	connectHandler := &workerConnectHandler{worker: worker}
	path, handler := maxcpuconnect.NewMaxCPUHandler(connectHandler)
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	lis, err := net.Listen("unix", socket)
	if err != nil {
		panic(err)
	}
	srv := &http.Server{Handler: mux}
	go srv.Serve(lis)
	cleanup := func() {
		srv.Close()
		lis.Close()
		_ = os.Remove(socket)
	}
	return srv, cleanup
}

func TestMakeClient(t *testing.T) {
	socket := path.Join(t.TempDir(), "test_maxcpu.sock")
	_, cleanup := startTestConnectServer(socket)
	defer cleanup()
	time.Sleep(100 * time.Millisecond) // サーバ起動待ち

	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return net.DialTimeout("unix", socket, 1*time.Second)
			},
		},
	}
	baseURL := "http://unix"
	client := maxcpuconnect.NewMaxCPUClient(httpClient, baseURL)
	if client == nil {
		t.Fatal("client is nil")
	}

	req := connect.NewRequest(&emptypb.Empty{})
	resp, err := client.Hello(t.Context(), req)
	if err != nil {
		t.Fatalf("Hello RPC failed: %v", err)
	}
	if resp.Msg.Message == "" {
		t.Errorf("unexpected Hello response: %v", resp.Msg.Message)
	}
}
