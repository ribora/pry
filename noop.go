package pry

type noopLogger struct{}

func Noop() Logger { return noopLogger{} }

func (noopLogger) Trace(_ any, _ ...Option) {}
func (noopLogger) Debug(_ any, _ ...Option) {}
func (noopLogger) Info(_ any, _ ...Option)  {}
func (noopLogger) Warn(_ any, _ ...Option)  {}
func (noopLogger) Error(_ any, _ ...Option) {}
