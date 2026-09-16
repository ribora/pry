package slog_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"gitlab.com/gorib/failure"
	"gitlab.com/gorib/pry"
	pryslog "gitlab.com/gorib/pry/slog"
)

func capture(t *testing.T, level pry.Level) (*bytes.Buffer, pry.Logger) {
	t.Helper()
	buf := &bytes.Buffer{}
	handler := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: pryslog.LevelTrace})
	return buf, pry.New(level, pryslog.New(handler))
}

func decoded(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &got); err != nil {
		t.Fatalf("decode %q: %s", buf.String(), err)
	}
	delete(got, "time")
	return got
}

func TestRecordReachesTheHandler(t *testing.T) {
	buf, l := capture(t, pry.LevelTrace)

	l.Warn("retrying", pry.Field("attempt", 3))

	got := decoded(t, buf)
	if got["level"] != "WARN" || got["msg"] != "retrying" || got["attempt"] != float64(3) {
		t.Fatalf("event: %v", got)
	}
}

// Deliberately unlike the zerolog driver, which leaves the message empty.
func TestErrorAsMessageBecomesTheMessage(t *testing.T) {
	buf, l := capture(t, pry.LevelTrace)

	l.Error(failure.Wrap("%w: id={id}", failure.New("thing is broken"), 42))

	got := decoded(t, buf)
	if got["msg"] != "thing is broken: id=42" {
		t.Fatalf("msg: %v", got["msg"])
	}
	payload, ok := got["payload"].(map[string]any)
	if !ok || payload["id"] != float64(42) {
		t.Fatalf("payload: %v", got["payload"])
	}
}

func TestTraceMapsBelowDebug(t *testing.T) {
	buf, l := capture(t, pry.LevelTrace)

	l.Trace("entering")

	if got := decoded(t, buf)["level"]; got != "DEBUG-4" {
		t.Fatalf("level: %v", got)
	}
}

// The handler keeps its own gate, so the stricter of the two levels wins.
func TestHandlerLevelStillApplies(t *testing.T) {
	buf := &bytes.Buffer{}
	handler := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelError})
	l := pry.New(pry.LevelTrace, pryslog.New(handler))

	l.Info("server started")

	if buf.Len() != 0 {
		t.Fatalf("handler level ignored: %q", buf.String())
	}
}
