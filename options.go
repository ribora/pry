package pry

import (
	"io"
)

func WithCaller() LoggerOption {
	return func(l Logger) {
		l.(*logger).l = l.(*logger).l.With().Caller().Logger()
	}
}

func WithFormat(w io.Writer) LoggerOption {
	return func(l Logger) {
		l.(*logger).format = w
	}
}

func ToChannels(w ...io.Writer) LoggerOption {
	return func(l Logger) {
		l.(*logger).channels = w
	}
}
