package w3c

// ParseTraceparent reads a W3C traceparent header:
// "00-{32 hex trace_id}-{16 hex span_id}-{2 hex flags}".
func ParseTraceparent(tp string) (traceID, spanID string, ok bool) {
	if len(tp) < 55 {
		return "", "", false
	}
	if tp[2] != '-' || tp[35] != '-' || tp[52] != '-' {
		return "", "", false
	}
	return tp[3:35], tp[36:52], true
}
