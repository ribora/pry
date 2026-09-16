package pry

// ToSinks attaches side channels — Sentry and anything else that receives
// every record. Output format and caller belong to the driver.
func ToSinks(sinks ...Sink) LoggerOption {
	return func(l *logger) { l.sinks = sinks }
}
