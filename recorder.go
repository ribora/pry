package pry

import "sync"

// Deprecated: Entry is Record. Kept so a suite that named the type still builds.
type Entry = Record

// Recorder is a Logger that keeps what it was told, for a test that asserts on
// logging. It is a Driver under the real logger, so options are applied by the
// code production runs.
func Recorder() *LogRecorder {
	r := &LogRecorder{}
	r.Logger = New(LevelTrace, r)
	return r
}

type LogRecorder struct {
	Logger

	mu      sync.Mutex
	records []Record
}

func (r *LogRecorder) Log(record Record) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, record)
}

func (r *LogRecorder) Entries() []Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Record(nil), r.records...)
}

func (r *LogRecorder) At(level Level) []Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	var found []Record
	for _, record := range r.records {
		if record.Level == level {
			found = append(found, record)
		}
	}
	return found
}

func (r *LogRecorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = nil
}
