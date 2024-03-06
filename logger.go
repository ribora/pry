package pry

import (
	"fmt"
	"io"

	"github.com/rs/zerolog"

	"gitlab.com/gorib/pry/formatter"
)

type LoggerOption func(l Logger)

type Option func(e Event)

type Logger interface {
	Trace(message any, opts ...Option)
	Debug(message any, opts ...Option)
	Info(message any, opts ...Option)
	Warn(message any, opts ...Option)
	Error(message any, opts ...Option)
}

func New(level string, options ...LoggerOption) (*logger, error) {
	lvl, err := zerolog.ParseLevel(level)
	if err != nil {
		return nil, err
	}
	l := &logger{l: zerolog.New(nil).Level(lvl).With().Timestamp().Logger(), format: formatter.Plain()}
	for _, option := range options {
		option(l)
	}
	l.l = l.l.Output(zerolog.MultiLevelWriter(append([]io.Writer{l.format}, l.channels...)...))

	return l, nil
}

type logger struct {
	l        zerolog.Logger
	format   io.Writer
	channels []io.Writer
}

func (l *logger) Trace(message any, opts ...Option) {
	l.log(zerolog.TraceLevel, message, opts...)
}

func (l *logger) Debug(message any, opts ...Option) {
	l.log(zerolog.DebugLevel, message, opts...)
}

func (l *logger) Info(message any, opts ...Option) {
	l.log(zerolog.InfoLevel, message, opts...)
}

func (l *logger) Warn(message any, opts ...Option) {
	l.log(zerolog.WarnLevel, message, opts...)
}

func (l *logger) Error(message any, opts ...Option) {
	l.log(zerolog.ErrorLevel, message, opts...)
}

func (l *logger) log(level zerolog.Level, message any, opts ...Option) {
	e := &event{e: l.l.WithLevel(level)}
	for _, opt := range opts {
		opt(e)
	}
	if err, ok := message.(error); ok {
		e.e.Err(err).Send()
	} else {
		e.e.Msg(fmt.Sprintf("%s", message))
	}
}
