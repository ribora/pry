package pry_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ribora/failure"
	"github.com/ribora/pry"
	pryzerolog "github.com/ribora/pry/zerolog"
	"github.com/ribora/session"
)

// Pins what the logger emits.

var errThing = failure.New("thing is broken")

// New returns an unexported type, so a test that needs Close names the shape.
type closable interface {
	pry.Logger
	Close() error
}

func capture(t *testing.T, level pry.Level, opts ...pry.LoggerOption) (*bytes.Buffer, closable) {
	t.Helper()
	buf := &bytes.Buffer{}
	return buf, pry.New(level, pryzerolog.New(buf), opts...)
}

// The timestamp key depends on a global zerolog variable the Json format sets
// process-wide, so neither name is part of what is pinned here.
func decoded(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatal("nothing was written")
	}
	if strings.Contains(line, "\n") {
		t.Fatalf("more than one event: %q", line)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("decode %q: %s", line, err)
	}
	delete(got, "time")
	delete(got, "@timestamp")
	return got
}

func TestEmittedEvent(t *testing.T) {
	withPayload := failure.Wrap("%w: id={id}", errThing, 42)

	cases := []struct {
		name string
		log  func(l pry.Logger)
		want map[string]any
	}{
		{
			name: "message only",
			log:  func(l pry.Logger) { l.Info("server started") },
			want: map[string]any{"level": "info", "message": "server started"},
		},
		{
			name: "error as message carries no message",
			log:  func(l pry.Logger) { l.Error(errThing) },
			want: map[string]any{"level": "error", "error": "thing is broken"},
		},
		{
			name: "message with Err",
			log:  func(l pry.Logger) { l.Error("request failed", pry.Err(errThing)) },
			want: map[string]any{"level": "error", "error": "thing is broken", "message": "request failed"},
		},
		{
			name: "fields are flattened onto the event",
			log: func(l pry.Logger) {
				l.Warn("retrying", pry.Field("attempt", 3), pry.Field("facility", "A"))
			},
			want: map[string]any{"level": "warn", "message": "retrying", "attempt": float64(3), "facility": "A"},
		},
		{
			name: "Ctx contributes nothing to the line",
			log: func(l pry.Logger) {
				ctx := session.NewContextWithSession(context.Background(), session.Session{Uri: "/v1/orders", Method: "POST"})
				l.Info("server started", pry.Ctx(ctx))
			},
			want: map[string]any{"level": "info", "message": "server started"},
		},
		{
			name: "Ctx without a session is inert",
			log:  func(l pry.Logger) { l.Info("server started", pry.Ctx(context.Background())) },
			want: map[string]any{"level": "info", "message": "server started"},
		},
		{
			name: "failure payload is attached from Err",
			log:  func(l pry.Logger) { l.Error("request failed", pry.Err(withPayload)) },
			want: map[string]any{
				"level":   "error",
				"error":   "thing is broken: id=42",
				"message": "request failed",
				"payload": map[string]any{"id": float64(42)},
			},
		},
		{
			name: "failure payload is attached from an error message",
			log:  func(l pry.Logger) { l.Error(withPayload) },
			want: map[string]any{
				"level":   "error",
				"error":   "thing is broken: id=42",
				"payload": map[string]any{"id": float64(42)},
			},
		},
		{
			name: "trace is the lowest level emitted",
			log:  func(l pry.Logger) { l.Trace("entering") },
			want: map[string]any{"level": "trace", "message": "entering"},
		},
		{
			name: "debug",
			log:  func(l pry.Logger) { l.Debug("cache miss") },
			want: map[string]any{"level": "debug", "message": "cache miss"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			buf, l := capture(t, pry.LevelTrace)
			c.log(l)
			got := decoded(t, buf)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("event:\n got: %v\nwant: %v", got, c.want)
			}
		})
	}
}

// A change to the Logger.X -> log -> Driver.Log chain moves the skip count.
func TestCallerPointsAtTheCallSite(t *testing.T) {
	buf := &bytes.Buffer{}
	l := pry.New(pry.LevelTrace, pryzerolog.New(buf, pryzerolog.WithCaller()))

	_, file, line, ok := runtime.Caller(0)
	l.Info("server started") // has to stay on the line directly below, nothing between
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	caller, _ := decoded(t, buf)["caller"].(string)
	if caller == "" {
		t.Fatal("no caller field")
	}
	if !strings.HasSuffix(file, "logger_test.go") {
		t.Fatalf("unexpected test file: %s", file)
	}
	if !strings.Contains(caller, "logger_test.go:") {
		t.Fatalf("caller points elsewhere: %s", caller)
	}
	want := line + 1
	if !strings.HasSuffix(caller, ":"+strconv.Itoa(want)) {
		t.Fatalf("caller: %s, want line %d", caller, want)
	}
}

func TestLevelSuppressesTheLine(t *testing.T) {
	buf, l := capture(t, pry.LevelWarn)

	l.Info("server started")

	if buf.Len() != 0 {
		t.Fatalf("info was written at warn level: %q", buf.String())
	}
}

type fakeSink struct {
	mu      sync.Mutex
	records []pry.Record
	closed  bool
}

func (s *fakeSink) Write(r pry.Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, r)
}

func (s *fakeSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *fakeSink) written() []pry.Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]pry.Record(nil), s.records...)
}

func TestSinkReceivesTheRecord(t *testing.T) {
	sink := &fakeSink{}
	_, l := capture(t, pry.LevelTrace, pry.ToSinks(sink))
	ctx := session.NewContextWithSession(context.Background(), session.Session{Uri: "/v1/orders"})

	l.Info("server started", pry.Field("attempt", 3), pry.Err(errThing), pry.Ctx(ctx))
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %s", err)
	}

	written := sink.written()
	if len(written) != 1 {
		t.Fatalf("writes: %d, want 1", len(written))
	}
	got := written[0]
	if got.Level != pry.LevelInfo {
		t.Errorf("level: %s", got.Level)
	}
	if got.Message != "server started" {
		t.Errorf("message: %q", got.Message)
	}
	if got.Session == nil || got.Session.Uri != "/v1/orders" {
		t.Errorf("session: %v", got.Session)
	}
	if !errors.Is(got.Err, errThing) {
		t.Errorf("err: %v", got.Err)
	}
	if value, ok := got.Field("attempt"); !ok || value != 3 {
		t.Errorf("attempt: %v, %v", value, ok)
	}
	if !sink.closed {
		t.Error("Close did not reach the sink")
	}
}

// An error-as-message reaches the sink with an empty message.
func TestSinkGetsNoMessageForAnErrorAsMessage(t *testing.T) {
	sink := &fakeSink{}
	_, l := capture(t, pry.LevelTrace, pry.ToSinks(sink))

	l.Error(errThing)
	_ = l.Close()

	written := sink.written()
	if len(written) != 1 {
		t.Fatalf("writes: %d, want 1", len(written))
	}
	if written[0].Message != "" {
		t.Errorf("message: %q, want empty", written[0].Message)
	}
}

// One gate, in the logger: a sink cannot ask for records below its level.
func TestSinkDoesNotSeeSuppressedRecords(t *testing.T) {
	sink := &fakeSink{}
	buf, l := capture(t, pry.LevelWarn, pry.ToSinks(sink))

	l.Info("server started")
	_ = l.Close()

	if buf.Len() != 0 {
		t.Fatalf("info was written at warn level: %q", buf.String())
	}
	if len(sink.written()) != 0 {
		t.Fatalf("sink writes: %d, want 0", len(sink.written()))
	}
}

// The session in a context is mutated in place by transport middleware, and a
// sink reads the record from another goroutine.
func TestSessionIsCopiedIntoTheRecord(t *testing.T) {
	sink := &fakeSink{}
	_, l := capture(t, pry.LevelTrace, pry.ToSinks(sink))
	ctx := session.NewContextWithSession(context.Background(), session.Session{Uri: "/v1/orders"})

	l.Info("server started", pry.Ctx(ctx))
	session.FromContext(ctx).Uri = "/v1/changed"
	_ = l.Close()

	if got := sink.written()[0].Session.Uri; got != "/v1/orders" {
		t.Fatalf("uri: %q, want the value at log time", got)
	}
}

func BenchmarkSuppressedDebug(b *testing.B) {
	l := pry.New(pry.LevelInfo, pryzerolog.New(io.Discard))
	b.ReportAllocs()
	for b.Loop() {
		l.Debug("cache miss", pry.Field("key", "user:42"))
	}
}

func BenchmarkEmittedInfo(b *testing.B) {
	l := pry.New(pry.LevelInfo, pryzerolog.New(io.Discard))
	b.ReportAllocs()
	for b.Loop() {
		l.Info("cache miss", pry.Field("key", "user:42"))
	}
}
