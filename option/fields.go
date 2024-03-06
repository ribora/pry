package option

const (
	CorrelationId = "correlation_id"
	Url           = "url"
	Method        = "method"
	Body          = "body"
	UserAgent     = "user_agent"
	UserLanguage  = "user_language"
	SourceIp      = "source_ip"
)

var DefaultExcludedFields = []string{
	CorrelationId,
	Url,
	Method,
	Body,
	UserAgent,
	UserLanguage,
	SourceIp,
}
