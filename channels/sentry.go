package channels

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/rs/zerolog"

	"gitlab.com/gorib/pry/option"
)

func Sentry(dsn, env string, levels ...string) (zerolog.LevelWriter, error) {
	var logLevels []zerolog.Level
	if len(levels) == 0 {
		logLevels = []zerolog.Level{zerolog.PanicLevel, zerolog.FatalLevel, zerolog.ErrorLevel}
	} else {
		for _, level := range levels {
			if lvl, err := zerolog.ParseLevel(level); err != nil {
				return nil, err
			} else {
				logLevels = append(logLevels, lvl)
			}
		}
	}

	var hub *sentry.Hub
	if dsn != "" {
		client, err := sentry.NewClient(sentry.ClientOptions{Dsn: dsn, Environment: env, Debug: true})
		if err != nil {
			return nil, err
		}
		hub = sentry.NewHub(nil, sentry.NewScope())
		hub.BindClient(client)
	}

	return &sentryChannel{
		hub:    hub,
		levels: logLevels,
	}, nil
}

type sentryChannel struct {
	io.Writer
	hub    *sentry.Hub
	levels []zerolog.Level
}

func (s sentryChannel) WriteLevel(level zerolog.Level, p []byte) (n int, err error) {
	n = len(p)
	if !slices.Contains(s.levels, level) {
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
	}
	for key, value := range payload {
		switch key {
		case zerolog.MessageFieldName:
			event.Message = fmt.Sprintf("%v", value)
		case zerolog.ErrorFieldName:
			event.Exception = append(event.Exception, sentry.Exception{
				Type:  fmt.Sprintf("%v: %v", payload[zerolog.MessageFieldName], value),
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
