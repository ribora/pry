// Package zerolog renders pry records with rs/zerolog. It is the default
// driver and the only package here that imports zerolog.
package zerolog

import (
	"io"
	"time"

	"github.com/rs/zerolog"

	"github.com/ribora/failure"
	"github.com/ribora/pry"
)

var levels = map[pry.Level]zerolog.Level{
	pry.LevelTrace: zerolog.TraceLevel,
	pry.LevelDebug: zerolog.DebugLevel,
	pry.LevelInfo:  zerolog.InfoLevel,
	pry.LevelWarn:  zerolog.WarnLevel,
	pry.LevelError: zerolog.ErrorLevel,
}

type Option func(d *Driver)

// WithCaller adds the file and line of the call site.
//
// The skip count walks Logger.X -> log -> Driver.Log -> here, so it belongs to
// that chain rather than to this file. TestCallerPointsAtTheCallSite guards it.
func WithCaller() Option {
	return func(d *Driver) { d.l = d.l.With().CallerWithSkipFrameCount(5).Logger() }
}

// New builds the driver. zerolog is opened at trace because the gate is the
// logger's — filtering twice would silently make the stricter level win.
func New(out io.Writer, options ...Option) *Driver {
	d := &Driver{l: zerolog.New(out).Level(zerolog.TraceLevel).With().Timestamp().Logger()}
	for _, option := range options {
		option(d)
	}
	return d
}

type Driver struct {
	l zerolog.Logger
}

func (d *Driver) Log(r pry.Record) {
	e := d.l.WithLevel(levels[r.Level])

	// Interface marshals through reflection; zerolog has a typed appender per kind.
	for _, attr := range r.Attrs {
		switch v := attr.Value.(type) {
		case string:
			e = e.Str(attr.Key, v)
		case int:
			e = e.Int(attr.Key, v)
		case int64:
			e = e.Int64(attr.Key, v)
		case float64:
			e = e.Float64(attr.Key, v)
		case bool:
			e = e.Bool(attr.Key, v)
		case time.Duration:
			e = e.Dur(attr.Key, v)
		case time.Time:
			e = e.Time(attr.Key, v)
		case error:
			e = e.AnErr(attr.Key, v)
		default:
			e = e.Interface(attr.Key, v)
		}
	}
	if fe, ok := failure.FromError(r.Err); ok {
		if payload := fe.Payload(); len(payload) > 0 {
			e = e.Fields(map[string]any{"payload": payload})
		}
	}

	if r.Message == "" && r.Err != nil {
		e.Err(r.Err).Send()
		return
	}
	if r.Err != nil {
		e = e.Err(r.Err)
	}
	e.Msg(r.Message)
}
