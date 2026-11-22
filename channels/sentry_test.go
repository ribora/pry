package channels

import (
	"context"
	"errors"
	"fmt"
	"go/build"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/ribora/failure"
	"github.com/ribora/pry"
	"github.com/ribora/session"
)

type fakeTransport struct{ events []*sentry.Event }

func (t *fakeTransport) Flush(time.Duration) bool              { return true }
func (t *fakeTransport) FlushWithContext(context.Context) bool { return true }
func (t *fakeTransport) Configure(sentry.ClientOptions)        {}
func (t *fakeTransport) SendEvent(event *sentry.Event)         { t.events = append(t.events, event) }
func (t *fakeTransport) Close()                                {}

func newTestChannel(t *testing.T, opts ...SentryOption) (*sentryChannel, *fakeTransport) {
	t.Helper()
	tr := &fakeTransport{}
	c, err := Sentry("trace", opts...)
	if err != nil {
		t.Fatal(err)
	}
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn:       "http://key@localhost/1",
		Transport: tr,
	})
	if err != nil {
		t.Fatal(err)
	}
	c.hub = sentry.NewHub(nil, sentry.NewScope())
	c.hub.BindClient(client)
	return c, tr
}

func testSession() *session.Session {
	return &session.Session{
		Uri:           "/orders/42",
		Method:        "POST",
		Body:          `{"secret":"ααααααααα"}`,
		CorrelationId: "corr-1",
		SourceIp:      "10.0.0.1",
	}
}

func testContext() context.Context {
	return session.WithTraceparent(
		context.Background(),
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	)
}

// The record carries the error as it was raised, so the sink parses it.
func TestPlainErrorIsParsedIntoAFailure(t *testing.T) {
	c, tr := newTestChannel(t)
	c.Write(pry.Record{Level: pry.LevelError, Err: errors.New("boom")})
	if len(tr.events) != 1 {
		t.Fatalf("events: %d", len(tr.events))
	}
	e := tr.events[0]
	if e.Exception[0].Type != "boom" || e.Exception[0].Value != "boom" {
		t.Fatalf("exception: %+v", e.Exception[0])
	}
}

// failure.Parse captures a stack when there is none, which is what applyError
// relies on for grouping.
func TestEveryErrorArrivesWithAStacktrace(t *testing.T) {
	c, tr := newTestChannel(t)

	c.Write(pry.Record{Level: pry.LevelError, Err: failure.New("guard is missing")})

	e := tr.events[0]
	if e.Exception[0].Stacktrace == nil || len(e.Exception[0].Stacktrace.Frames) == 0 {
		t.Fatalf("stacktrace: %+v", e.Exception[0])
	}
	if len(e.Fingerprint) != 0 {
		t.Fatalf("fingerprint: %v, want none when there are frames", e.Fingerprint)
	}
}

func TestEmptyEventDropped(t *testing.T) {
	c, tr := newTestChannel(t)
	c.Write(pry.Record{Level: pry.LevelError})
	if len(tr.events) != 0 {
		t.Fatalf("events: %d", len(tr.events))
	}
}

func TestFailureEvent(t *testing.T) {
	c, tr := newTestChannel(t)
	err := failure.Parse(failure.Wrap("order {order_id} not found", 42))
	c.Write(pry.Record{
		Level:   pry.LevelError,
		Message: "lookup failed",
		Err:     err,
		Attrs:   []pry.Attr{{Key: "attempt", Value: 2}},
		Session: testSession(),
		Ctx:     testContext(),
	})

	e := tr.events[0]
	if e.Tags["correlation_id"] != "corr-1" || e.Tags["method"] != "POST" {
		t.Fatalf("tags: %v", e.Tags)
	}
	if e.Tags["error_type"] != "order_not_found" {
		t.Fatalf("error_type: %v", e.Tags["error_type"])
	}
	if e.Tags["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace tag: %v", e.Tags)
	}
	trace := e.Contexts["trace"]
	if fmt.Sprint(trace["trace_id"]) != "4bf92f3577b34da6a3ce929d0e0e4736" ||
		fmt.Sprint(trace["span_id"]) != "00f067aa0ba902b7" {
		t.Fatalf("trace context: %v", trace)
	}
	if e.Contexts["payload"]["order_id"] != 42 {
		t.Fatalf("payload: %v", e.Contexts["payload"])
	}
	if e.Contexts["fields"]["attempt"] != 2 {
		t.Fatalf("fields: %v", e.Contexts["fields"])
	}
	if e.Message != "lookup failed" {
		t.Fatalf("message: %q", e.Message)
	}
	if e.Request.Data != `{"secret":"ααααααααα"}` {
		t.Fatalf("body: %q", e.Request.Data)
	}
	if e.Request.URL != "/orders/42" || e.Request.Method != "POST" {
		t.Fatalf("request: %+v", e.Request)
	}
	if len(e.Fingerprint) != 0 {
		t.Fatalf("fingerprint must stay default when a stacktrace exists: %v", e.Fingerprint)
	}
	if len(e.Exception[0].Stacktrace.Frames) == 0 {
		t.Fatal("no frames")
	}
	if h := e.Exception[0].Mechanism.Handled; h == nil || !*h {
		t.Fatalf("mechanism: %+v", e.Exception[0].Mechanism)
	}
}

func TestConvertFrames(t *testing.T) {
	c := &sentryChannel{appPrefix: "gitlab.com/gotrg"}
	frames := c.convertFrames([]failure.Frame{
		{Function: "github.com/ribora/pry.(*logger).log", File: "/src/pry/logger.go", Line: 10},
		{Function: "gitlab.com/gotrg/orders.(*Service).Get", File: "/src/orders/service.go", Line: 20},
		{Function: "main.main", File: "/src/main.go", Line: 5},
	})
	if len(frames) != 3 {
		t.Fatalf("frames must be kept: %+v", frames)
	}
	if frames[0].Function != "main" || frames[2].Function != "(*logger).log" {
		t.Fatalf("frames must be oldest-first: %+v", frames)
	}
	if frames[1].Module != "gitlab.com/gotrg/orders" {
		t.Fatalf("module: %+v", frames[1])
	}
	if frames[1].AbsPath != "/src/orders/service.go" || frames[1].Lineno != 20 {
		t.Fatalf("path: %+v", frames[1])
	}
	if !frames[1].InApp {
		t.Fatalf("app frame must be in-app: %+v", frames[1])
	}
	if frames[2].InApp {
		t.Fatalf("library frame must be a system frame: %+v", frames[2])
	}
}

func TestConvertFramesWithoutAppPrefix(t *testing.T) {
	c := &sentryChannel{}
	frames := c.convertFrames([]failure.Frame{
		{Function: "testing.tRunner", File: build.Default.GOROOT + "/src/testing/testing.go", Line: 1},
		{Function: "gitlab.com/gotrg/orders.(*Service).Get", File: "/src/orders/service.go", Line: 20},
	})
	// Without a prefix the sdk decides: goroot is a system frame, the rest is not.
	if !frames[0].InApp {
		t.Fatalf("app frame must keep the sdk verdict: %+v", frames[0])
	}
	if frames[1].InApp {
		t.Fatalf("goroot frame must stay a system frame: %+v", frames[1])
	}
}

func TestPanicUnhandled(t *testing.T) {
	c, tr := newTestChannel(t)
	c.Write(pry.Record{Level: pry.LevelError, Err: failure.Panic("kaboom")})
	m := tr.events[0].Exception[0].Mechanism
	if m.Type != "panic" || m.Handled == nil || *m.Handled {
		t.Fatalf("mechanism: %+v", m)
	}
}
