package channels

import (
	"context"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/rs/zerolog"

	"gitlab.com/gorib/failure"
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

func (s sentryChannel) Send(level zerolog.Level, ctx context.Context, message string, err error, fields ...map[string]any) {
	if s.level > level {
		return
	}
	if s.hub == nil {
		return
	}

	event := &sentry.Event{
		Timestamp: time.Now(),
		Tags:      make(map[string]string),
		Extra:     make(map[string]any),
		Request: &sentry.Request{
			Headers: make(map[string]string),
		},
		Level: sentryLevels[level],
	}
	if ss := session.FromContext(ctx); ss != nil {
		event.Tags["correlation_id"] = ss.CorrelationId
		event.Request.URL = ss.Uri
		event.Request.Method = ss.Method
		event.Request.Data = ss.Body
		event.Request.Headers["User-Agent"] = ss.UserAgent
		event.Request.Headers["Accept-Language"] = ss.UserLanguage
		event.Request.Env = map[string]string{"REMOTE_ADDR": ss.SourceIp}
	}
	for _, field := range fields {
		for key, value := range field {
			event.Extra[key] = value
		}
	}

	var msg []string
	if message != "" {
		msg = append(msg, message)
	}
	if err != nil {
		var errorType string
		var errorValue string
		msg = append(msg, err.Error())
		stack := &sentry.Stacktrace{}
		if fe, ok := failure.FromError(err); ok {
			errorType = fe.Type()
			errorValue = fe.Error()
			st := fe.Stack()
			frames := make([]sentry.Frame, len(st))
			for i, f := range st {
				frames[len(st)-1-i] = sentry.Frame{
					Function: f.Function,
					Filename: f.File,
					Lineno:   f.Line,
				}
			}
			stack.Frames = frames
		} else {
			errorType = "error"
			errorValue = err.Error()
			st := sentry.NewStacktrace()
			if st != nil {
				var frames []sentry.Frame
				for _, frame := range st.Frames {
					if frame.Module != "github.com/rs/zerolog" && strings.Index(frame.Module, "gitlab.com/gorib/pry") != 0 {
						frames = append(frames, frame)
					}
				}
				stack.Frames = frames
			}
		}
		event.Exception = append(event.Exception, sentry.Exception{
			Type:       errorType,
			Value:      errorValue,
			Stacktrace: stack,
		})
	}
	event.Message = strings.Join(msg, ": ")

	s.hub.CaptureEvent(event)
}

func (s sentryChannel) Close() error {
	if s.hub != nil {
		s.hub.Flush(3 * time.Second)
	}
	return nil
}
