package statworker

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync/atomic"
	"time"

	"github.com/bufbuild/connect-go"
	"github.com/kazeburo/mackerel-plugin-maxcpu/maxcpu"
	"google.golang.org/protobuf/types/known/emptypb"
)

// The `round` function rounds the input float to the nearest integer and subtracts 1.
// This offset is applied to adjust for zero-based indexing in certain calculations.
func round(f float64) int64 {
	return int64(math.Round(f)) - 1
}

func (*Worker) Hello(_ context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[maxcpu.HelloResponse], error) {
	return connect.NewResponse(&maxcpu.HelloResponse{Message: "OK"}), nil
}

func (w *Worker) GetStats(_ context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[maxcpu.StatsResponse], error) {
	stats, err := w.stats()
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&maxcpu.StatsResponse{Metrics: stats}), nil
}

func (w *Worker) stats() ([]*maxcpu.Metric, error) {
	// reset idle time
	atomic.StoreInt64(&w.idleTime, 0)

	w.lock.Lock()
	defer w.lock.Unlock()

	var usages sort.Float64Slice
	var i int64
	var total float64
	for i = 1; i < historySize; i++ {
		if w.usages[i] != nil {
			usages = append(usages, w.usages[i].Usage)
			total += w.usages[i].Usage
		}
	}

	// clear stats
	current := w.usages[w.current]
	w.current = 0
	w.usages = make([]*cpuUsage, historySize)
	w.usages[0] = current

	res := make([]*maxcpu.Metric, 0)

	if len(usages) < 2 {
		return res, fmt.Errorf("calculating now")
	}

	sort.Sort(usages)
	flen := float64(len(usages))
	epoch := time.Now().Unix()

	res = append(res, &maxcpu.Metric{
		Key:    "max",
		Metric: usages[round(flen)],
		Epoch:  epoch,
	})
	res = append(res, &maxcpu.Metric{
		Key:    "min",
		Metric: usages[0],
		Epoch:  epoch,
	})
	res = append(res, &maxcpu.Metric{
		Key:    "avg",
		Metric: total / flen,
		Epoch:  epoch,
	})
	res = append(res, &maxcpu.Metric{
		Key:    "90pt",
		Metric: usages[round(flen*0.90)],
		Epoch:  epoch,
	})
	res = append(res, &maxcpu.Metric{
		Key:    "75pt",
		Metric: usages[round(flen*0.75)],
		Epoch:  epoch,
	})

	return res, nil
}
