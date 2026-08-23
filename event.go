package pry

import (
	"context"

	"github.com/rs/zerolog"

	"gitlab.com/gorib/failure"
	"gitlab.com/gorib/session"
)

type Event any

type event struct {
	Event
	ctx    context.Context
	err    error
	fields []map[string]any
	e      *zerolog.Event
}

func (e *event) fetchPayload(err error) *event {
	if fe, ok := failure.FromError(err); ok {
		if p := fe.Payload(); len(p) > 0 {
			e.e = e.e.Fields(map[string]any{"payload": p})
		}
	}
	return e
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
	}
}

func Field(key string, value any) Option {
	return func(e Event) {
		e.(*event).fields = append(e.(*event).fields, map[string]any{key: value})
	}
}
