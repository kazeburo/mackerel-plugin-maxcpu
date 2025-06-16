package statworker

import (
	"sync/atomic"
)

func (w *Worker) IdleTime() int64 {
	return atomic.LoadInt64(&w.idleTime)
}
