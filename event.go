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
		e.(*event).ctx = ctx
	}
}

func Field(key string, value any) Option {
	return func(e Event) {
		e.(*event).fields = append(e.(*event).fields, map[string]any{key: value})
	}
}
