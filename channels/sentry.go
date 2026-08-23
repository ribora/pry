package channels

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/rs/zerolog"

	"gitlab.com/gorib/failure"
	"gitlab.com/gorib/pry/internal/w3c"
	"gitlab.com/gorib/session"
)

var sentryLevels = map[zerolog.Level]sentry.Level{
	zerolog.TraceLevel: sentry.LevelDebug,
	zerolog.DebugLevel: sentry.LevelDebug,
	zerolog.InfoLevel:  sentry.LevelInfo,
	zerolog.WarnLevel:  sentry.LevelWarning,
	zerolog.ErrorLevel: sentry.LevelError,
	zerolog.FatalLevel: sentry.LevelFatal,
	zerolog.PanicLevel: sentry.LevelFatal,
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
	lvl, err := zerolog.ParseLevel(level)
	if err != nil {
		return nil, err
	}
	l := &sentryChannel{
		level: lvl,
	}
	for _, opt := range options {
		if err = opt(l); err != nil {
			return nil, err
		}
	}

	return l, nil
}

type sentryChannel struct {
	hub   *sentry.Hub
	level zerolog.Level
}

func (s *sentryChannel) Send(level zerolog.Level, ctx context.Context, message string, err error, fields ...map[string]any) {
	if s.level > level || s.hub == nil {
		return
	}
	if message == "" && err == nil {
		return
	}

	event := sentry.NewEvent()
	event.Timestamp = time.Now()
	event.Level = sentryLevels[level]
	event.Logger = "gorib"
	event.Message = message

	if len(fields) > 0 {
		merged := make(sentry.Context)
		for _, field := range fields {
			maps.Copy(merged, field)
		}
		event.Contexts["fields"] = merged
	}

	hub := s.hub
	if ss := session.FromContext(ctx); ss != nil {
		s.applySession(event, ss)
		if traceId, spanId, ok := w3c.ParseTraceparent(ss.Traceparent); ok {
			event.Tags["trace_id"] = traceId
			// The scope owns the trace context, so linking the event to the
			// incoming trace means cloning the hub instead of writing the context.
			if pc, ok := propagationContext(traceId, spanId); ok {
				hub = hub.Clone()
				hub.Scope().SetPropagationContext(pc)
			}
		}
	}
	if err != nil {
		applyError(event, err)
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

func applyError(event *sentry.Event, err error) {
	errorType, errorValue := fmt.Sprintf("%T", err), err.Error()
	mechanism := &sentry.Mechanism{Type: "generic", Handled: sentry.Pointer(true)}
	var frames []sentry.Frame

	if fe, ok := failure.FromError(err); ok {
		errorType, errorValue = fe.Type(), fe.Error()
		frames = convertFrames(fe.Stack())
		if payload := fe.Payload(); len(payload) > 0 {
			event.Contexts["payload"] = sentry.Context(payload)
		}
	}
	if errors.Is(err, failure.ErrPanic) {
		mechanism.Type = "panic"
		mechanism.SetUnhandled()
	}
	event.Tags["error_type"] = errorType

	exception := sentry.Exception{Type: errorType, Value: errorValue, Mechanism: mechanism}
	if len(frames) > 0 {
		exception.Stacktrace = &sentry.Stacktrace{Frames: frames}
	} else {
		// Without a stacktrace Sentry groups by type and value, and values carry
		// rendered payload data, so every occurrence would open its own issue.
		event.Fingerprint = []string{errorType}
	}
	event.Exception = append(event.Exception, exception)
}

func convertFrames(stack []failure.Frame) []sentry.Frame {
	frames := make([]sentry.Frame, 0, len(stack))
	for _, f := range stack {
		frame := sentry.NewFrame(runtime.Frame{
			Function: f.Function,
			File:     f.File,
			Line:     f.Line,
		})
		// Library frames only describe how the error reached the channel: they stay visible,
		// but as system frames they never become the culprit and stay out of grouping.
		if strings.HasPrefix(f.Function, "gitlab.com/gorib") {
			frame.InApp = false
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
