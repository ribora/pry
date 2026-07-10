package pry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/rs/zerolog"

	"gitlab.com/gorib/failure"
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

type Channel interface {
	Send(level zerolog.Level, ctx context.Context, message string, err error, fields ...map[string]any)
	Close() error
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
	l.l = l.l.Output(l.format)

	return l, nil
}

type logger struct {
	l        zerolog.Logger
	format   io.Writer
	channels []Channel
	wg       sync.WaitGroup
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
	for _, f := range e.fields {
		e.e = e.e.Fields(f)
	}

	var msg string
	if err, ok := message.(error); ok {
		e.err = err
		e.fetchPayload(e.err)
		e.e.Err(err).Send()
	} else {
		if e.err != nil {
			e.e = e.e.Err(e.err)
		}
		msg = fmt.Sprintf("%v", message)
		e.fetchPayload(e.err)
		e.e.Msg(msg)
	}

	ctx := context.Background()
	if e.ctx != nil {
		ctx = e.ctx
	}
	chanErr := failure.Parse(e.err)
	for _, channel := range l.channels {
		l.wg.Add(1)
		c := channel
		go func() {
			defer l.wg.Done()
			c.Send(level, ctx, msg, chanErr, e.fields...)
		}()
	}
}

func (l *logger) Close() error {
	l.wg.Wait()
	var errs []error
	for _, channel := range l.channels {
		if err := channel.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
