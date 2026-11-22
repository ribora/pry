package pry

import "github.com/ribora/failure"

// Ordered so a sink can filter with a comparison. The zero value is LevelDebug.
type Level int8

const (
	LevelTrace Level = iota - 1
	LevelDebug
	LevelInfo
	LevelWarn
	LevelError
)

var ErrLevel = failure.New("unknown log level")

var levelNames = map[Level]string{
	LevelTrace: "trace",
	LevelDebug: "debug",
	LevelInfo:  "info",
	LevelWarn:  "warn",
	LevelError: "error",
}

func (l Level) String() string {
	if name, ok := levelNames[l]; ok {
		return name
	}
	return "unknown"
}

func ParseLevel(name string) (Level, error) {
	for level, known := range levelNames {
		if known == name {
			return level, nil
		}
	}
	return LevelInfo, failure.Wrap("%w: {level}", ErrLevel, name)
}
