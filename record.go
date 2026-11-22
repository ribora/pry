package pry

import (
	"context"

	"github.com/ribora/session"
)

// Named Attr because Field is the option that appends one.
type Attr struct {
	Key   string
	Value any
}

// Record is what a driver renders and a sink forwards — options write it, so a
// driver outside this package can read everything they set.
type Record struct {
	Level   Level
	Message string
	Err     error
	Attrs   []Attr

	// Ctx is for a driver that needs it — slog.Handler.Handle takes one.
	Ctx context.Context
	// A copy, because a sink reads it from another goroutine.
	Session *session.Session
}

func (r Record) Field(key string) (any, bool) {
	for _, attr := range r.Attrs {
		if attr.Key == key {
			return attr.Value, true
		}
	}
	return nil, false
}

type Option func(r *Record)

func Err(err error) Option {
	return func(r *Record) { r.Err = err }
}

func Ctx(ctx context.Context) Option {
	return func(r *Record) {
		r.Ctx = ctx
		if s := session.FromContext(ctx); s != nil {
			copied := *s
			r.Session = &copied
		}
	}
}

func Field(key string, value any) Option {
	return func(r *Record) { r.Attrs = append(r.Attrs, Attr{Key: key, Value: value}) }
}
