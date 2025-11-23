package statworker

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestWorkerWithUsages(usages []float64, current int64) *Worker {
	w := &Worker{
		usages:  make([]*cpuUsage, historySize),
		current: current,
	}
	for i, u := range usages {
		if i >= int(historySize) {
			break
		}
		w.usages[i] = &cpuUsage{Usage: u}
	}
	w.lock = sync.Mutex{}
	return w
}

func TestMStats_NotEnoughData(t *testing.T) {
	w := newTestWorkerWithUsages([]float64{10.0}, 0)
	resp, err := w.stats()
	if err != nil {
		// "calculating now" エラーが返ることを期待
		if err.Error() != "calculating now" {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if len(resp) != 0 {
		t.Fatalf("expected 0 value, got %d", len(resp))
	}
}

func TestMStats_EnoughData(t *testing.T) {
	usages := []float64{0, 10, 20, 30, 40, 50}
	w := newTestWorkerWithUsages(usages, 5)
	resp, err := w.stats()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp) != 5 {
		t.Errorf("expected 5 metrics, got %d", len(resp))
	}
	keys := map[string]bool{}
	for _, m := range resp {
		keys[m.Key] = true
		if m.Epoch == 0 {
			t.Errorf("expected non-zero epoch")
		}
	}
	for _, k := range []string{"max", "min", "avg", "90pt", "75pt"} {
		if !keys[k] {
			t.Errorf("missing metric key: %s", k)
		}
	}
}

func TestMStats_ResetsIdleTime(t *testing.T) {
	w := newTestWorkerWithUsages([]float64{0, 10, 20}, 2)
	atomic.StoreInt64(&w.idleTime, 123)
	_, _ = w.stats()
	if got := atomic.LoadInt64(&w.idleTime); got != 0 {
		t.Errorf("expected idleTime reset to 0, got %d", got)
	}
}

func TestMStats_ClearsStatsExceptCurrent(t *testing.T) {
	usages := []float64{0, 10, 20, 30}
	w := newTestWorkerWithUsages(usages, 2)
	_, err := w.stats()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.current != 0 {
		t.Errorf("expected current to be 0, got %d", w.current)
	}
	if w.usages[0] == nil || w.usages[0].Usage != 20 {
		t.Errorf("expected usages[0] to be the previous current, got %+v", w.usages[0])
	}
	for i := 1; i < int(historySize); i++ {
		if w.usages[i] != nil {
			t.Errorf("expected usages[%d] to be nil, got %+v", i, w.usages[i])
		}
	}
}

func TestMStats_ConcurrentAccess(t *testing.T) {
	usages := []float64{0, 10, 20, 30, 40, 50}
	w := newTestWorkerWithUsages(usages, 5)
	done := make(chan struct{})
	go func() {
		_, _ = w.stats()
		close(done)
	}()
	// Try to acquire the lock to ensure no deadlock
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("mStats deadlocked")
	}
}
