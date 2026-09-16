package pry

// Driver renders a record. The level gate, the fan-out to sinks and Close are
// the logger's.
//
// A driver holding something to release implements io.Closer, and Logger.Close
// calls it.
type Driver interface {
	Log(Record)
}

// Sink receives every record the logger emits, off the calling goroutine.
type Sink interface {
	Write(Record)
	Close() error
}

// Leveled is the optional half of Sink: a sink that answers it is sent nothing
// below that level, so its queue carries only what it would keep.
//
// It narrows and never widens. A sink that could ask for more than the logger
// emits would be a second place deciding the level.
type Leveled interface {
	Level() Level
}
