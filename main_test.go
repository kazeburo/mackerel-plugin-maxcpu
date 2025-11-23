package main

import (
	"net"
	"os"
	"path"
	"testing"
	"time"

	"github.com/kazeburo/mackerel-plugin-maxcpu/internal/statworker"
	"github.com/kazeburo/mackerel-plugin-maxcpu/maxcpu"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

func startTestGRPCServer(socket string) (*grpc.Server, func()) {
	_ = os.Remove(socket)

	worker := statworker.New()
	server := grpc.NewServer()
	maxcpu.RegisterMaxCPUServer(server, worker)

	lis, err := net.Listen("unix", socket)
	if err != nil {
		panic(err)
	}
	go server.Serve(lis)
	cleanup := func() {
		server.Stop()
		lis.Close()
		_ = os.Remove(socket)
	}
	return server, cleanup
}

func TestMakeClient(t *testing.T) {
	socket := path.Join(t.TempDir(), "test_maxcpu.sock")
	_, cleanup := startTestGRPCServer(socket)
	defer cleanup()
	time.Sleep(100 * time.Millisecond) // サーバ起動待ち

	client, err := makeClient(socket)
	if err != nil {
		t.Fatalf("makeClient failed: %v", err)
	}
	if client == nil {
		t.Fatal("client is nil")
	}

	_, err = client.Hello(t.Context(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("Hello RPC failed: %v", err)
	}
}
