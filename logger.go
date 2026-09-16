package pry

import (
	"errors"
	"fmt"
	"io"
	"sync"
)

type LoggerOption func(l *logger)

type Logger interface {
	Trace(message any, opts ...Option)
	Debug(message any, opts ...Option)
	Info(message any, opts ...Option)
	Warn(message any, opts ...Option)
	Error(message any, opts ...Option)
}

// New builds a logger over a driver. The caller parses the level, with ParseLevel.
func New(level Level, driver Driver, options ...LoggerOption) *logger {
	l := &logger{level: level, driver: driver}
	for _, option := range options {
		option(l)
	}
	for _, sink := range l.sinks {
		l.workers = append(l.workers, newWorker(sink, level))
	}
	return l
}

type logger struct {
	level   Level
	driver  Driver
	sinks   []Sink
	workers []*worker
	once    sync.Once
}

func (l *logger) Trace(message any, opts ...Option) { l.log(LevelTrace, message, opts) }
func (l *logger) Debug(message any, opts ...Option) { l.log(LevelDebug, message, opts) }
func (l *logger) Info(message any, opts ...Option)  { l.log(LevelInfo, message, opts) }
func (l *logger) Warn(message any, opts ...Option)  { l.log(LevelWarn, message, opts) }
func (l *logger) Error(message any, opts ...Option) { l.log(LevelError, message, opts) }

func (l *logger) log(level Level, message any, opts []Option) {
	if level < l.level {
		return
	}

	r := Record{Level: level}
	for _, opt := range opts {
		opt(&r)
	}
	// An error as the message wins over the Err option and leaves the message empty.
	if err, ok := message.(error); ok {
		r.Err = err
	} else {
		r.Message = fmt.Sprintf("%v", message)
	}

	l.driver.Log(r)

	for _, w := range l.workers {
		w.send(r)
	}
}

// Dropped counts the records a full sink queue cost, across every sink. It is
// the only trace a dropped record leaves.
func (l *logger) Dropped() uint64 {
	var total uint64
	for _, w := range l.workers {
		total += w.droppedSoFar()
	}
	return total
}

func (l *logger) Close() error {
	var errs []error
	l.once.Do(func() {
		var dropped uint64
		for _, w := range l.workers {
			dropped += w.stop()
			if err := w.sink.Close(); err != nil {
				errs = append(errs, err)
			}
		}
		// Through the driver, because the queues are stopping. Not an error: a
		// shutdown that panics over a lost log line is worse than the lost line.
		if dropped > 0 {
			l.driver.Log(Record{
				Level:   LevelWarn,
				Message: "pry: sink queue overflowed, records were dropped",
				Attrs:   []Attr{{Key: "dropped", Value: dropped}},
			})
		}
		if closer, ok := l.driver.(io.Closer); ok {
			if err := closer.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	})
	return errors.Join(errs...)
}
