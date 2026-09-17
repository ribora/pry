package channels

import (
	"encoding/hex"
	"errors"
	"fmt"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"

	"gitlab.com/gorib/failure"
	"gitlab.com/gorib/pry"
	"gitlab.com/gorib/pry/internal/w3c"
	"gitlab.com/gorib/session"
)

var sentryLevels = map[pry.Level]sentry.Level{
	pry.LevelTrace: sentry.LevelDebug,
	pry.LevelDebug: sentry.LevelDebug,
	pry.LevelInfo:  sentry.LevelInfo,
	pry.LevelWarn:  sentry.LevelWarning,
	pry.LevelError: sentry.LevelError,
}

type SentryOption func(l *sentryChannel) error

type sentryConnectOption func(o *sentry.ClientOptions) error

func WithSentryService(name string) sentryConnectOption {
	return func(o *sentry.ClientOptions) error {
		o.ServerName = name
		return nil
	}
}

func WithSentryRelease(release string) sentryConnectOption {
	return func(o *sentry.ClientOptions) error {
		o.Release = release
		return nil
	}
}

func WithSentryScrubber(scrub func(event *sentry.Event) *sentry.Event) sentryConnectOption {
	return func(o *sentry.ClientOptions) error {
		if scrub == nil {
			return nil
		}
		o.BeforeSend = func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
			return scrub(event)
		}
		return nil
	}
}

func WithSentryConnection(dsn, env string, opts ...sentryConnectOption) SentryOption {
	return func(l *sentryChannel) error {
		if dsn == "" {
			return nil
		}
		o := &sentry.ClientOptions{Dsn: dsn, Environment: env}
		for _, opt := range opts {
			if err := opt(o); err != nil {
				return err
			}
		}
		client, err := sentry.NewClient(*o)
		if err != nil {
			return err
		}
		l.hub = sentry.NewHub(nil, sentry.NewScope())
		l.hub.BindClient(client)
		return nil
	}
}

func Sentry(level string, options ...SentryOption) (*sentryChannel, error) {
	lvl, err := pry.ParseLevel(level)
	if err != nil {
		return nil, err
	}

	l := &sentryChannel{
		level: lvl,
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		l.appPrefix = bi.Main.Path
	}
	for _, opt := range options {
		if err = opt(l); err != nil {
			return nil, err
		}
	}

	return l, nil
}

type sentryChannel struct {
	hub       *sentry.Hub
	level     pry.Level
	appPrefix string
}

// Level keeps the queue from carrying what Write would discard on arrival — at
// LOGLEVEL=trace that is every request the service serves.
func (s *sentryChannel) Level() pry.Level { return s.level }

// The check stays: Write is part of the Sink interface and anyone may call it,
// including a test and a logger that does not ask about Level.
func (s *sentryChannel) Write(r pry.Record) {
	if s.level > r.Level || s.hub == nil {
		return
	}
	if r.Message == "" && r.Err == nil {
		return
	}

	event := sentry.NewEvent()
	event.Timestamp = time.Now()
	event.Level = sentryLevels[r.Level]
	event.Logger = "gorib"
	event.Message = r.Message

	if len(r.Attrs) > 0 {
		merged := make(sentry.Context, len(r.Attrs))
		for _, attr := range r.Attrs {
			merged[attr.Key] = attr.Value
		}
		event.Contexts["fields"] = merged
	}

	hub := s.hub
	if ss := r.Session; ss != nil {
		s.applySession(event, ss)
	}
	if traceId, spanId, ok := w3c.ParseTraceparent(session.TraceparentFromContext(r.Ctx)); ok {
		event.Tags["trace_id"] = traceId
		// The scope owns the trace context, so linking the event to the
		// incoming trace means cloning the hub instead of writing the context.
		if pc, ok := propagationContext(traceId, spanId); ok {
			hub = hub.Clone()
			hub.Scope().SetPropagationContext(pc)
		}
	}
	// The record carries the error as it was raised, so the failure is built here.
	if r.Err != nil {
		s.applyError(event, failure.Parse(r.Err))
	}

	hub.CaptureEvent(event)
}

func (s *sentryChannel) applySession(event *sentry.Event, ss *session.Session) {
	if ss.CorrelationId != "" {
		event.Tags["correlation_id"] = ss.CorrelationId
	}
	if ss.Method != "" {
		event.Tags["method"] = ss.Method
	}
	if ss.SourceIp != "" {
		event.User = sentry.User{IPAddress: ss.SourceIp}
	}
	if ss.Uri == "" && ss.Method == "" {
		return
	}
	event.Request = &sentry.Request{URL: ss.Uri, Method: ss.Method, Data: ss.Body}

	headers := make(map[string]string, 2)
	if ss.UserAgent != "" {
		headers["User-Agent"] = ss.UserAgent
	}
	if ss.UserLanguage != "" {
		headers["Accept-Language"] = ss.UserLanguage
	}
	if len(headers) > 0 {
		event.Request.Headers = headers
	}
}

func propagationContext(traceId, spanId string) (sentry.PropagationContext, bool) {
	pc := sentry.PropagationContext{
		DynamicSamplingContext: sentry.DynamicSamplingContext{Frozen: true},
	}
	if _, err := hex.Decode(pc.TraceID[:], []byte(traceId)); err != nil {
		return sentry.PropagationContext{}, false
	}
	if _, err := hex.Decode(pc.SpanID[:], []byte(spanId)); err != nil {
		return sentry.PropagationContext{}, false
	}
	return pc, true
}

func (s *sentryChannel) applyError(event *sentry.Event, err error) {
	errorType, errorValue := fmt.Sprintf("%T", err), err.Error()
	mechanism := &sentry.Mechanism{Type: "generic", Handled: new(true)}
	var frames []sentry.Frame

	if fe, ok := failure.FromError(err); ok {
		errorType, errorValue = fe.Type(), fe.Error()
		frames = s.convertFrames(fe.Stack())
		if payload := fe.Payload(); len(payload) > 0 {
			event.Contexts["payload"] = payload
		}
	}
	if errors.Is(err, failure.ErrPanic) {
		mechanism.Type = "panic"
		mechanism.SetUnhandled()
	}
	event.Tags["error_type"] = errorType

	// No fingerprint fallback: Write parses the error, and failure.Parse captures
	// a stack when there is none, so grouping always has frames to work with.
	exception := sentry.Exception{Type: errorType, Value: errorValue, Mechanism: mechanism}
	if len(frames) > 0 {
		exception.Stacktrace = &sentry.Stacktrace{Frames: frames}
	}
	event.Exception = append(event.Exception, exception)
}

func (s *sentryChannel) convertFrames(stack []failure.Frame) []sentry.Frame {
	frames := make([]sentry.Frame, 0, len(stack))
	for _, f := range stack {
		frame := sentry.NewFrame(runtime.Frame{
			Function: f.Function,
			File:     f.File,
			Line:     f.Line,
		})
		if s.appPrefix != "" {
			frame.InApp = frame.Module == "main" || strings.HasPrefix(frame.Module, s.appPrefix)
		}
		frames = append(frames, frame)
	}
	// Sentry expects frames oldest-first; failure.Stack returns newest-first.
	slices.Reverse(frames)
	return frames
}

func (s *sentryChannel) Close() error {
	if s.hub != nil {
		s.hub.Flush(3 * time.Second)
	}
	return nil
}
