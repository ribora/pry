package pry_test

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"

	"gitlab.com/gorib/pry"
	pryzerolog "gitlab.com/gorib/pry/zerolog"
)

// A sink that will not move until it is let go.
type blockingSink struct {
	release chan struct{}

	mu      sync.Mutex
	written int
}

func (s *blockingSink) Write(pry.Record) {
	<-s.release
	s.mu.Lock()
	defer s.mu.Unlock()
	s.written++
}

func (s *blockingSink) Close() error { return nil }

func (s *blockingSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.written
}

// The queue is what bounds a slow sink: it costs records, not the process.
func TestASlowSinkCostsRecordsAndNotTheProcess(t *testing.T) {
	sink := &blockingSink{release: make(chan struct{})}
	l := pry.New(pry.LevelInfo, pryzerolog.New(&bytes.Buffer{}), pry.ToSinks(sink))

	// Well past the queue: everything over it is dropped rather than queued.
	const sent = 5000
	for range sent {
		l.Info("server started")
	}

	if dropped := l.Dropped(); dropped == 0 {
		t.Fatal("nothing was dropped, so the queue did not bound anything")
	}
	close(sink.release)
	_ = l.Close()

	if got := sink.count() + int(l.Dropped()); got != sent {
		t.Errorf("written + dropped = %d, want %d — a record went missing unaccounted", got, sent)
	}
}

// The line the driver writes is the only trace a drop leaves, so it has to be
// there and it has to carry the number.
func TestCloseSaysHowManyWereDropped(t *testing.T) {
	sink := &blockingSink{release: make(chan struct{})}
	out := &bytes.Buffer{}
	l := pry.New(pry.LevelInfo, pryzerolog.New(out), pry.ToSinks(sink))

	for range 5000 {
		l.Info("server started")
	}
	close(sink.release)
	_ = l.Close()

	last := out.String()
	if !strings.Contains(last, "sink queue overflowed") {
		t.Fatalf("Close said nothing about the drops:\n%s", last[max(0, len(last)-400):])
	}
	if !strings.Contains(last, `"dropped"`) {
		t.Error("the report carries no count")
	}
}

// Logging after Close must not panic, and what it costs must be counted.
func TestLoggingAfterCloseIsCountedAndDoesNotPanic(t *testing.T) {
	sink := &fakeSink{}
	l := pry.New(pry.LevelInfo, pryzerolog.New(&bytes.Buffer{}), pry.ToSinks(sink))

	l.Info("before")
	_ = l.Close()
	before := l.Dropped()

	l.Info("after")

	if l.Dropped() != before+1 {
		t.Errorf("a record logged after Close was neither delivered nor counted")
	}
	if len(sink.written()) != 1 {
		t.Errorf("writes = %d, want only the one from before Close", len(sink.written()))
	}
}

// Close is wired into the container's shutdown, where it can be reached twice.
func TestCloseTwice(t *testing.T) {
	sink := &fakeSink{}
	l := pry.New(pry.LevelInfo, pryzerolog.New(&bytes.Buffer{}), pry.ToSinks(sink))

	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("the second Close failed: %s", err)
	}
}

type silentSink struct{}

func (silentSink) Write(pry.Record) {}
func (silentSink) Close() error     { return nil }

type leveledSink struct {
	fakeSink
	level pry.Level
}

func (s *leveledSink) Level() pry.Level { return s.level }

// At LOGLEVEL=trace with Sentry at error, a queue without this would carry one
// record per request served in order to discard each of them on arrival.
func TestASinkIsNotSentWhatItDeclines(t *testing.T) {
	sink := &leveledSink{level: pry.LevelError}
	l := pry.New(pry.LevelTrace, pryzerolog.New(&bytes.Buffer{}), pry.ToSinks(sink))

	for range 1000 {
		l.Info("GET /v1/orders >> {}")
	}
	l.Error("boom")
	_ = l.Close()

	written := sink.written()
	if len(written) != 1 {
		t.Fatalf("writes = %d, want only the error", len(written))
	}
	if written[0].Message != "boom" {
		t.Errorf("wrote %q", written[0].Message)
	}
	// Declining is not losing: the count must not claim a thousand casualties.
	if dropped := l.Dropped(); dropped != 0 {
		t.Errorf("dropped = %d, want 0 — what a sink declined was never lost", dropped)
	}
}

// A sink can narrow what it takes and never widen it: two places deciding the
// level is the thing the single gate exists to prevent.
func TestASinkCannotAskBelowTheLoggersLevel(t *testing.T) {
	sink := &leveledSink{level: pry.LevelTrace}
	l := pry.New(pry.LevelWarn, pryzerolog.New(&bytes.Buffer{}), pry.ToSinks(sink))

	l.Info("server started")
	l.Warn("retrying")
	_ = l.Close()

	written := sink.written()
	if len(written) != 1 || written[0].Level != pry.LevelWarn {
		t.Fatalf("the sink saw %v, want only the warning", written)
	}
}

// The emitting path with a sink attached, driver included.
func BenchmarkEmittedWithASink(b *testing.B) {
	l := pry.New(pry.LevelInfo, pryzerolog.New(io.Discard), pry.ToSinks(silentSink{}))
	b.ReportAllocs()
	for b.Loop() {
		l.Info("cache miss", pry.Field("key", "user:42"))
	}
}

// The shape this replaced, measured beside it so the trade is a number rather
// than an argument. It is only the fan-out — neither includes the driver.
func BenchmarkFanOutStrategies(b *testing.B) {
	sink := silentSink{}
	record := pry.Record{Level: pry.LevelInfo, Message: "cache miss"}

	b.Run("goroutine per record", func(b *testing.B) {
		var wg sync.WaitGroup
		b.ReportAllocs()
		for b.Loop() {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sink.Write(record)
			}()
		}
		wg.Wait()
	})

	b.Run("channel to one goroutine", func(b *testing.B) {
		records := make(chan pry.Record, 256)
		done := make(chan struct{})
		go func() {
			defer close(done)
			for r := range records {
				sink.Write(r)
			}
		}()
		b.ReportAllocs()
		for b.Loop() {
			select {
			case records <- record:
			default:
			}
		}
		close(records)
		<-done
	})
}

// One goroutine per sink means the sink sees records in the order they were
// logged, which a goroutine per record could not promise.
func TestRecordsArriveInOrder(t *testing.T) {
	sink := &fakeSink{}
	l := pry.New(pry.LevelInfo, pryzerolog.New(&bytes.Buffer{}), pry.ToSinks(sink))

	for i := range 100 {
		l.Info("line", pry.Field("n", i))
	}
	_ = l.Close()

	written := sink.written()
	if len(written) != 100 {
		t.Fatalf("writes = %d, want 100", len(written))
	}
	for i, record := range written {
		if n, _ := record.Field("n"); n != i {
			t.Fatalf("record %d carries n=%v — the order is not the one they were logged in", i, n)
		}
	}
}
