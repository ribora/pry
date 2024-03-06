package pry

import (
	"context"

	"github.com/rs/zerolog"

	"gitlab.com/gorib/pry/option"
	"gitlab.com/gorib/session"
)

type Event interface{}

type event struct {
	Event
	e *zerolog.Event
}

func Err(err error) Option {
	return func(e Event) {
		e.(*event).e.Err(err)
	}
}

func Ctx(ctx context.Context) Option {
	return func(e Event) {
		s := session.FromContext(ctx)
		if s == nil {
			return
		}
		e.(*event).e.Str(option.Url, s.Uri)
		e.(*event).e.Str(option.Method, s.Method)
		e.(*event).e.Str(option.Body, s.Body)
		e.(*event).e.Str(option.CorrelationId, s.CorrelationId)
		e.(*event).e.Str(option.UserAgent, s.UserAgent)
		e.(*event).e.Str(option.UserLanguage, s.UserLanguage)
		e.(*event).e.Str(option.SourceIp, s.SourceIp)
	}
}

func Field(key string, value any) Option {
	return func(e Event) {
		e.(*event).e.Fields([]any{key, value})
	}
}
