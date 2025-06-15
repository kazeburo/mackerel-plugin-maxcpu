package statworker

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"sync/atomic"
	"time"

	"github.com/kazeburo/mackerel-plugin-maxcpu/maxcpu"
)

// The `round` function rounds the input float to the nearest integer and subtracts 1.
// This offset is applied to adjust for zero-based indexing in certain calculations.
func round(f float64) int64 {
	return int64(math.Round(f)) - 1
}

func (w *Worker) MGet(keys []string) (*maxcpu.Response, error) {
	if len(keys) == 0 || len(keys) > 1 {
		return nil, fmt.Errorf("no arguments or too many arguments for get")
	}
	switch keys[0] {
	case "hello":
		return w.mHello(keys[0])
	case "stats":
		return w.mStats(keys[0])
	default:
		return maxcpu.NotFound, nil
	}
}

func (w *Worker) mHello(key string) (*maxcpu.Response, error) {
	res := maxcpu.GetHelloResponse{
		Message: "OK",
	}
	b, err := json.Marshal(res)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}
	mres := &maxcpu.Response{}
	mres.Values = append(mres.Values, &maxcpu.Value{
		Key:  key,
		Data: b,
	})
	return mres, nil
}

func (w *Worker) mStats(key string) (*maxcpu.Response, error) {
	// reset idle time
	atomic.StoreInt64(&w.idleTime, 0)

	w.lock.Lock()
	defer w.lock.Unlock()

	var usages sort.Float64Slice
	var i int64
	var total float64
	for i = 1; i < maxUsage; i++ {
		if w.usages[i] != nil {
			usages = append(usages, w.usages[i].Usage)
			total += w.usages[i].Usage
		}
	}

	// clear stats
	current := w.usages[w.current]
	w.current = 0
	w.usages = make([]*cpuUsage, maxUsage)
	w.usages[0] = current

	var res maxcpu.GetStatsResponse

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

	res.Metrics = append(res.Metrics, &maxcpu.Metric{
		Key:    "max",
		Metric: usages[round(flen)],
		Epoch:  epoch,
	})
	res.Metrics = append(res.Metrics, &maxcpu.Metric{
		Key:    "min",
		Metric: usages[0],
		Epoch:  epoch,
	})
	res.Metrics = append(res.Metrics, &maxcpu.Metric{
		Key:    "avg",
		Metric: total / flen,
		Epoch:  epoch,
	})
	res.Metrics = append(res.Metrics, &maxcpu.Metric{
		Key:    "90pt",
		Metric: usages[round(flen*0.90)],
		Epoch:  epoch,
	})
	res.Metrics = append(res.Metrics, &maxcpu.Metric{
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
