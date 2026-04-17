# pry

Structured logger for Go built on [zerolog](https://github.com/rs/zerolog). Supports pluggable output formatters, side-channel delivery (Sentry / Glitchtip), and structured fields via options.

## Installation

```
go get gitlab.com/gorib/pry
```

## Usage

### Creating a logger

```go
l, err := pry.New("info")
```

The level string is parsed by zerolog: `trace`, `debug`, `info`, `warn`, `error`.

### Logging

Each method accepts a message and zero or more options:

```go
l.Info("server started")
l.Warn("retrying", pry.Field("attempt", 3))
l.Error(err)                                    // error as message
l.Error("request failed", pry.Err(err))         // string message + error
```

| Method | zerolog level |
|--------|--------------|
| `Trace` | TraceLevel |
| `Debug` | DebugLevel |
| `Info`  | InfoLevel  |
| `Warn`  | WarnLevel  |
| `Error` | ErrorLevel |

### Options

Options are applied per log call:

| Option | Description |
|--------|-------------|
| `pry.Err(err)` | Attaches an error to the event. |
| `pry.Field(key, value)` | Adds a structured key/value field. |
| `pry.Ctx(ctx)` | Attaches request context (session metadata). See [Session context](#session-context). |

### Logger options

Logger-level options are passed to `pry.New`:

| Option | Description |
|--------|-------------|
| `pry.WithFormat(w)` | Sets the output writer / formatter. Default: `formatter.Plain()`. |
| `pry.WithCaller()` | Adds caller file and line to every event. |
| `pry.ToChannels(ch...)` | Adds side-channels (e.g., Sentry). |

## Formatters

```go
import "gitlab.com/gorib/pry/formatter"

// Human-readable console output with timestamps.
// Color is disabled automatically when stdout is not a TTY.
l, _ := pry.New("debug", pry.WithFormat(formatter.Plain()))

// JSON output to stdout. Sets zerolog.TimestampFieldName = "@timestamp".
l, _ := pry.New("debug", pry.WithFormat(formatter.Json()))
```

> **Note:** `formatter.Json()` sets the global `zerolog.TimestampFieldName` variable. This affects all zerolog loggers in the process.

## Channels

Channels receive every log event asynchronously (each send runs in a goroutine). The logger waits for all in-flight channel sends to complete on `Close`.

```go
l, _ := pry.New("info", pry.ToChannels(ch1, ch2))
// ...
_ = l.Close() // flush all channels before shutdown
```

### Sentry / Glitchtip

```go
import "gitlab.com/gorib/pry/channels"

sentry, err := channels.Sentry("error",
    channels.WithSentryConnection(dsn, "production",
        channels.WithSentryService("my-service"),
    ),
)

l, err := pry.New("debug", pry.ToChannels(sentry))
```

`Sentry(level, ...)` — only events at or above `level` are forwarded to Sentry.

`WithSentryConnection(dsn, env, opts...)` — configures the Sentry client. If `dsn` is empty the channel is created but silently does nothing.

`WithSentryService(name)` — sets `ServerName` in the Sentry client options.

When an error is a `failure.Error`, the Sentry event includes the structured stack trace and type code from `failure`. Otherwise a live stack trace is captured and internal pry/zerolog frames are filtered out.

## Session context

`pry.Ctx(ctx)` enriches the Sentry event with HTTP request metadata extracted from the session stored in the context:

```go
l.Error("payment failed", pry.Err(err), pry.Ctx(ctx))
```

The following fields are forwarded to Sentry when present:

| Session field | Sentry field |
|---------------|-------------|
| `CorrelationId` | `tags["correlation_id"]` |
| `Uri` | `request.url` |
| `Method` | `request.method` |
| `Body` | `request.data` |
| `UserAgent` | `request.headers["User-Agent"]` |
| `UserLanguage` | `request.headers["Accept-Language"]` |
| `SourceIp` | `request.env["REMOTE_ADDR"]` |

If the context carries no session, `Ctx` is a no-op.

## Closing

`logger` is not a `Logger` interface method — call it on the concrete `*logger` returned by `New`:

```go
l, _ := pry.New("info", pry.ToChannels(sentry))
defer func() { _ = l.Close() }()
```

`Close` blocks until all goroutines spawned for channel delivery finish, then calls `Close` on each channel (Sentry flushes with a 3-second timeout).
