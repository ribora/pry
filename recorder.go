package pry

import (
	"fmt"
	"sync"

	"gitlab.com/gorib/failure"
)

// Has to live here: every Option asserts Event to the unexported event type, so
// a Logger implemented elsewhere gets options it can never read.
func Recorder() *LogRecorder { return &LogRecorder{} }

type Level string

const (
	LevelTrace Level = "trace"
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

type Entry struct {
	Level   Level
	Message string
	Err     error
	Fields  map[string]any
}

func (e Entry) Field(key string) (any, bool) {
	value, ok := e.Fields[key]
	return value, ok
}

type LogRecorder struct {
	mu      sync.Mutex
	entries []Entry
}

func (r *LogRecorder) Trace(message any, opts ...Option) { r.record(LevelTrace, message, opts) }
func (r *LogRecorder) Debug(message any, opts ...Option) { r.record(LevelDebug, message, opts) }
func (r *LogRecorder) Info(message any, opts ...Option)  { r.record(LevelInfo, message, opts) }
func (r *LogRecorder) Warn(message any, opts ...Option)  { r.record(LevelWarn, message, opts) }
func (r *LogRecorder) Error(message any, opts ...Option) { r.record(LevelError, message, opts) }

func (r *LogRecorder) record(level Level, message any, opts []Option) {
	// The zerolog event stays nil: Err, Ctx and Field only write to the struct.
	e := &event{}
	for _, opt := range opts {
		opt(e)
	}

	entry := Entry{Level: level, Err: e.err}
	// logger.log sends an error-as-message with no message, so filling one in
	// would let a test assert something production never emits.
	if err, ok := message.(error); ok {
		entry.Err = err
	} else {
		entry.Message = fmt.Sprintf("%v", message)
	}

	fields := make(map[string]any)
	for _, group := range e.fields {
		for key, value := range group {
			fields[key] = value
		}
	}
	// The same "payload" field logger.log attaches from a failure error.
	if fe, ok := failure.FromError(entry.Err); ok {
		if payload := fe.Payload(); len(payload) > 0 {
			fields["payload"] = payload
		}
	}
	if len(fields) > 0 {
		entry.Fields = fields
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, entry)
}

func (r *LogRecorder) Entries() []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Entry(nil), r.entries...)
}

func (r *LogRecorder) At(level Level) []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	var found []Entry
	for _, entry := range r.entries {
		if entry.Level == level {
			found = append(found, entry)
		}
	}
	return found
}

func (r *LogRecorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = nil
}
