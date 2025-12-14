package channels

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/rs/zerolog"

	"gitlab.com/gorib/pry/option"
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

func Sentry(level string, options ...SentryOption) (io.Writer, error) {
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
	io.Writer
	hub   *sentry.Hub
	level zerolog.Level
}

func (s sentryChannel) WriteLevel(level zerolog.Level, p []byte) (n int, err error) {
	n = len(p)
	if s.level > level {
		return
	}
	if s.hub == nil {
		return
	}

	var payload map[string]any
	err = json.Unmarshal(p, &payload)
	if err != nil {
		return
	}

	event := &sentry.Event{
		Timestamp: time.Now(),
		Tags:      make(map[string]string),
		Extra:     make(map[string]any),
		Request:   &sentry.Request{},
		Level:     sentryLevels[level],
	}
	for key, value := range payload {
		switch key {
		case zerolog.MessageFieldName:
			event.Message = fmt.Sprintf("%v", value)
		case zerolog.ErrorFieldName:
			var msg string
			if payload[zerolog.MessageFieldName] != nil && payload[zerolog.MessageFieldName] != "" {
				msg = fmt.Sprintf("%v: %s", payload[zerolog.MessageFieldName], value)
			} else {
				msg = fmt.Sprintf("%v", value)
			}
			event.Exception = append(event.Exception, sentry.Exception{
				Type:  msg,
				Value: fmt.Sprintf("%v", payload[option.Url]),
				Stacktrace: func() *sentry.Stacktrace {
					stack := sentry.NewStacktrace()
					var frames []sentry.Frame
					for _, frame := range stack.Frames {
						if frame.Module != "github.com/rs/zerolog" && strings.Index(frame.Module, "gitlab.com/gorib/pry") != 0 {
							frames = append(frames, frame)
						}
					}
					stack.Frames = frames
					return stack
				}(),
			})
		case zerolog.LevelFieldName, zerolog.TimestampFieldName:
		case option.CorrelationId:
			event.Tags[key] = fmt.Sprintf("%v", value)
		case option.Url:
			event.Request.URL = fmt.Sprintf("%v", value)
		case option.Method:
			event.Request.Method = fmt.Sprintf("%v", value)
		case option.Body:
			event.Request.Data = fmt.Sprintf("%v", value)
		default:
			event.Extra[key] = fmt.Sprintf("%v", value)
		}
	}

	s.hub.CaptureEvent(event)
	s.hub.Flush(3 * time.Second)
	return
}
