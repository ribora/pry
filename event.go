package pry

import (
	"context"

	"github.com/rs/zerolog"

	"gitlab.com/gorib/session"
)

type Event interface{}

type event struct {
	Event
	ctx    context.Context
	err    error
	fields []map[string]any
	e      *zerolog.Event
}

func Err(err error) Option {
	return func(e Event) {
		e.(*event).err = err
	}
}

func Ctx(ctx context.Context) Option {
	return func(e Event) {
		s := session.FromContext(ctx)
		if s == nil {
			return
		}
		ev := e.(*event)
		ev.ctx = ctx
		fields := map[string]any{}
		if s.CorrelationId != "" {
			fields["correlation_id"] = s.CorrelationId
		}
		if traceID, spanID, ok := parseTraceparent(s.Traceparent); ok {
			fields["trace_id"] = traceID
			fields["span_id"] = spanID
		}
		if len(fields) > 0 {
			ev.fields = append(ev.fields, fields)
		}
	}
}

// W3C traceparent: "00-{32 hex trace_id}-{16 hex span_id}-{2 hex flags}".
func parseTraceparent(tp string) (traceID, spanID string, ok bool) {
	if len(tp) < 55 {
		return "", "", false
	}
	if tp[2] != '-' || tp[35] != '-' || tp[52] != '-' {
		return "", "", false
	}
	return tp[3:35], tp[36:52], true
}

func Field(key string, value any) Option {
	return func(e Event) {
		e.(*event).fields = append(e.(*event).fields, map[string]any{key: value})
	}
}
