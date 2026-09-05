package pry

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"gitlab.com/gorib/failure"
)

var _ Logger = Recorder()

// record builds an event with a nil zerolog field, which holds only while no
// option touches it. An option that does breaks this, not a service's suite.
func TestEveryOptionSurvivesTheRecorder(t *testing.T) {
	r := Recorder()

	r.Info("everything at once",
		Err(errors.New("broken")),
		Ctx(t.Context()),
		Field("customerId", 42),
	)

	entry := r.Entries()[0]
	if entry.Message != "everything at once" {
		t.Fatalf("message: %q", entry.Message)
	}
	if entry.Err == nil {
		t.Error("Err was dropped")
	}
	if value, ok := entry.Field("customerId"); !ok || value != 42 {
		t.Errorf("customerId: %v, %v", value, ok)
	}
}

func TestFieldsAreReadable(t *testing.T) {
	r := Recorder()

	r.Warn("Retrying guard update", Field("customerId", 42), Field("facility", "A"))

	entries := r.Entries()
	if len(entries) != 1 {
		t.Fatalf("entries: %d, want 1", len(entries))
	}
	if got, ok := entries[0].Field("customerId"); !ok || got != 42 {
		t.Fatalf("customerId: %v, %v", got, ok)
	}
	if got, ok := entries[0].Field("facility"); !ok || got != "A" {
		t.Fatalf("facility: %v, %v", got, ok)
	}
}

func TestErrOptionIsReadable(t *testing.T) {
	broken := errors.New("broken")
	r := Recorder()

	r.Error("Dropping guard update", Err(broken))

	if got := r.Entries()[0].Err; !errors.Is(got, broken) {
		t.Fatalf("err: %v", got)
	}
}

// logger.log sends this with no message, so the recorder must not invent one.
func TestErrorPassedAsTheMessageLandsUnderErrWithNoMessage(t *testing.T) {
	broken := errors.New("broken")
	r := Recorder()

	r.Error(broken)

	entry := r.Entries()[0]
	if !errors.Is(entry.Err, broken) {
		t.Fatalf("err: %v", entry.Err)
	}
	if entry.Message != "" {
		t.Fatalf("message: %q, want empty", entry.Message)
	}
}

// The real logger attaches a failure's payload as a field, and a scenario
// asserting on it should read the same thing here.
func TestPayloadIsAttachedTheWayTheLoggerAttachesIt(t *testing.T) {
	r := Recorder()

	r.Error("Dropping guard update", Err(failure.New("%w: {customerId}", errBroken, 42)))

	payload, ok := r.Entries()[0].Field("payload")
	if !ok {
		t.Fatal("payload: missing")
	}
	if fmt.Sprintf("%v", payload) == "" {
		t.Fatalf("payload: %v", payload)
	}
}

var errBroken = errors.New("broken")

func TestLevelIsRecorded(t *testing.T) {
	r := Recorder()

	r.Trace("t")
	r.Debug("d")
	r.Info("i")
	r.Warn("w")
	r.Error("e")

	want := []Level{LevelTrace, LevelDebug, LevelInfo, LevelWarn, LevelError}
	entries := r.Entries()
	if len(entries) != len(want) {
		t.Fatalf("entries: %d, want %d", len(entries), len(want))
	}
	for i, level := range want {
		if entries[i].Level != level {
			t.Fatalf("entry %d: %s, want %s", i, entries[i].Level, level)
		}
	}
}

func TestAtFiltersByLevel(t *testing.T) {
	r := Recorder()

	r.Info("fine")
	r.Warn("first")
	r.Warn("second")

	warnings := r.At(LevelWarn)
	if len(warnings) != 2 || warnings[0].Message != "first" || warnings[1].Message != "second" {
		t.Fatalf("warnings: %v", warnings)
	}
	if len(r.At(LevelError)) != 0 {
		t.Fatal("errors: want none")
	}
}

func TestEntriesIsACopy(t *testing.T) {
	r := Recorder()
	r.Info("first")

	entries := r.Entries()
	r.Info("second")

	if len(entries) != 1 {
		t.Fatalf("entries: %d, want the snapshot untouched", len(entries))
	}
}

func TestResetDropsTheHistory(t *testing.T) {
	r := Recorder()
	r.Info("first")

	r.Reset()

	if len(r.Entries()) != 0 {
		t.Fatal("entries: want none after reset")
	}
}

func TestMessageWithoutFieldsHasNoMap(t *testing.T) {
	r := Recorder()

	r.Info("plain")

	if _, ok := r.Entries()[0].Field("anything"); ok {
		t.Fatal("field: want none")
	}
}

func TestRecorderIsSafeForConcurrentUse(t *testing.T) {
	r := Recorder()
	var wg sync.WaitGroup

	for i := range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Warn("Failed to sync guard", Field("n", i))
		}()
	}
	wg.Wait()

	if len(r.At(LevelWarn)) != 50 {
		t.Fatalf("warnings: %d, want 50", len(r.At(LevelWarn)))
	}
}
