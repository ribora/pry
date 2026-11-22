// Package slog renders pry records through a log/slog handler.
//
// A JSONHandler writes time/msg/INFO where pry/zerolog writes
// time-or-@timestamp/message/info, so a pipeline keyed on the zerolog spelling
// needs HandlerOptions.ReplaceAttr. See the README.
package slog

import (
	"context"
	"log/slog"
	"time"

	"github.com/ribora/failure"
	"github.com/ribora/pry"
)

// slog's scale starts at Debug, so trace is an ordinary int below it.
const LevelTrace = slog.LevelDebug - 4

var levels = map[pry.Level]slog.Level{
	pry.LevelTrace: LevelTrace,
	pry.LevelDebug: slog.LevelDebug,
	pry.LevelInfo:  slog.LevelInfo,
	pry.LevelWarn:  slog.LevelWarn,
	pry.LevelError: slog.LevelError,
}

// New builds the driver over a handler the caller owns.
//
// The handler keeps its own level, so the effective one is the stricter of the
// two. Open the pry logger at trace to let the handler decide alone.
func New(handler slog.Handler) *Driver {
	return &Driver{handler: handler}
}

type Driver struct {
	handler slog.Handler
}

func (d *Driver) Log(r pry.Record) {
	ctx := r.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	level := levels[r.Level]
	if !d.handler.Enabled(ctx, level) {
		return
	}

	// An error with no message reads as `msg=` under most handlers.
	message := r.Message
	if message == "" && r.Err != nil {
		message = r.Err.Error()
	}

	record := slog.NewRecord(time.Now(), level, message, 0)
	for _, attr := range r.Attrs {
		record.AddAttrs(slog.Any(attr.Key, attr.Value))
	}
	if r.Err != nil {
		record.AddAttrs(slog.Any("error", r.Err))
		if fe, ok := failure.FromError(r.Err); ok {
			if payload := fe.Payload(); len(payload) > 0 {
				record.AddAttrs(slog.Any("payload", payload))
			}
		}
	}

	_ = d.handler.Handle(ctx, record)
}
