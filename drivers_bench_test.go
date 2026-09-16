package pry_test

import (
	"io"
	"log/slog"
	"testing"

	"gitlab.com/gorib/failure"
	"gitlab.com/gorib/pry"
	pryslog "gitlab.com/gorib/pry/slog"
	pryzerolog "gitlab.com/gorib/pry/zerolog"
)

func drivers() map[string]pry.Driver {
	return map[string]pry.Driver{
		"zerolog": pryzerolog.New(io.Discard),
		"slog":    pryslog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: pryslog.LevelTrace})),
	}
}

func BenchmarkDriverMessage(b *testing.B) {
	for name, driver := range drivers() {
		b.Run(name, func(b *testing.B) {
			l := pry.New(pry.LevelInfo, driver)
			b.ReportAllocs()
			for b.Loop() {
				l.Info("server started")
			}
		})
	}
}

func BenchmarkDriverFields(b *testing.B) {
	for name, driver := range drivers() {
		b.Run(name, func(b *testing.B) {
			l := pry.New(pry.LevelInfo, driver)
			b.ReportAllocs()
			for b.Loop() {
				l.Info("cache miss", pry.Field("key", "user:42"), pry.Field("attempt", 3))
			}
		})
	}
}

func BenchmarkDriverError(b *testing.B) {
	err := failure.Wrap("%w: id={id}", failure.New("thing is broken"), 42)
	for name, driver := range drivers() {
		b.Run(name, func(b *testing.B) {
			l := pry.New(pry.LevelInfo, driver)
			b.ReportAllocs()
			for b.Loop() {
				l.Error("request failed", pry.Err(err))
			}
		})
	}
}
