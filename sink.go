package pry

import (
	"sync/atomic"
)

// Deep enough that a burst of errors survives a sink that is briefly busy, and
// small enough to be worth nothing: a Record is a handful of words.
const sinkBuffer = 256

// One goroutine per sink: the sink sees records in the order they were logged,
// and one that slows down costs records rather than unbounded goroutines.
type worker struct {
	sink    Sink
	level   Level
	records chan Record
	quit    chan struct{}
	stopped chan struct{}
	closed  atomic.Bool
	dropped atomic.Uint64
}

// floor is the logger's own level, which a sink may raise and never lower.
func newWorker(sink Sink, floor Level) *worker {
	level := floor
	if leveled, ok := sink.(Leveled); ok && leveled.Level() > level {
		level = leveled.Level()
	}
	w := &worker{
		sink:    sink,
		level:   level,
		records: make(chan Record, sinkBuffer),
		quit:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	go w.run()
	return w
}

// Never blocks: logging must not become a source of latency, so a full queue
// costs a record rather than the request that produced it.
func (w *worker) send(r Record) {
	// Declined, not dropped: the count must not claim what nobody wanted.
	if r.Level < w.level {
		return
	}
	if w.closed.Load() {
		w.dropped.Add(1)
		return
	}
	select {
	case w.records <- r:
	default:
		w.dropped.Add(1)
	}
}

func (w *worker) run() {
	defer close(w.stopped)
	for {
		select {
		case r := <-w.records:
			w.sink.Write(r)
		case <-w.quit:
			w.drain()
			return
		}
	}
}

func (w *worker) drain() {
	for {
		select {
		case r := <-w.records:
			w.sink.Write(r)
		default:
			return
		}
	}
}

// The channel is never closed, so a log after Close is a counted drop instead of
// a panic on a closed channel.
func (w *worker) stop() uint64 {
	w.closed.Store(true)
	close(w.quit)
	<-w.stopped
	return w.dropped.Load()
}

func (w *worker) droppedSoFar() uint64 { return w.dropped.Load() }
