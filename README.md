# pry

Structured logger for Go. `pry` itself is the facade — a `Logger`, a `Record` and
the options that build one — and a **driver** renders that record. Side channels
(Sentry / Glitchtip) receive the same record as a `Sink`.

[zerolog](https://github.com/rs/zerolog) is the default driver and
[log/slog](https://pkg.go.dev/log/slog) is the other one. Only `pry/zerolog`
imports zerolog, so a binary that takes the slog driver never links it.

## Installation

```
go get github.com/ribora/pry
```

## Usage

### Creating a logger

```go
import (
    "github.com/ribora/pry"
    pryzerolog "github.com/ribora/pry/zerolog"
)

level, err := pry.ParseLevel("info")
if err != nil {
    return err
}
l := pry.New(level, pryzerolog.New(pryzerolog.Plain()))
```

Levels are `trace`, `debug`, `info`, `warn`, `error`. The logger's gate runs
before the record is built — a suppressed call costs ~2.6ns and allocates
nothing.

### Drivers

```go
pryzerolog.New(pryzerolog.Plain())   // console, colour when stdout is a TTY
pryzerolog.New(pryzerolog.Json())    // stdout as JSON
pryslog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: pryslog.LevelTrace}))
```

The zerolog driver is opened at trace, so the logger's level is the only gate.
A slog handler keeps its own level — `Info` when `HandlerOptions.Level` is
unset — and the stricter of the two wins, so open it at `pryslog.LevelTrace` to
let the logger decide alone.

Writing one is a method:

```go
type Driver interface {
    Log(Record)
}
```

A driver with something to release implements `io.Closer` and closing the
logger calls it. Measured on an M2 Max, per emitted line:

| | message | + 2 fields | + a failure error |
|---|---|---|---|
| `pry/zerolog` | 181 ns · 2 allocs | 305 ns · 4 allocs | 685 ns · 11 allocs |
| `pry/slog` | 303 ns · 2 allocs | 445 ns · 4 allocs | 1891 ns · 15 allocs |

zerolog is the default because of that last column; slog exists so a service
that wants the standard library is not forced off the rest of the package.

#### The two write different keys

Both emit JSON — `pryzerolog.Json()` and `slog.NewJSONHandler` — but not the
same JSON:

| | `pry/zerolog` | `pry/slog` + `JSONHandler` |
|---|---|---|
| timestamp | `time`, or `@timestamp` after `Json()` | `time` |
| message | `message` | `msg` |
| level | `info` | `INFO` |

Swapping the driver therefore compiles, runs, and keeps writing lines your log
pipeline may stop parsing — a failure you find in an incident rather than in a
build. `slog.HandlerOptions.ReplaceAttr` renames all three when you need the
zerolog spelling; there is no helper for it here, because which spelling is
right depends on what reads the logs.

### Logging

Each method accepts a message and zero or more options:

```go
l.Info("server started")
l.Warn("retrying", pry.Field("attempt", 3))
l.Error(err)                                    // error as message
l.Error("request failed", pry.Err(err))         // string message + error
```

| Method | Level | zerolog | slog |
|--------|-------|---------|------|
| `Trace` | `LevelTrace` | `TraceLevel` | `LevelDebug - 4` |
| `Debug` | `LevelDebug` | `DebugLevel` | `LevelDebug` |
| `Info`  | `LevelInfo`  | `InfoLevel`  | `LevelInfo` |
| `Warn`  | `LevelWarn`  | `WarnLevel`  | `LevelWarn` |
| `Error` | `LevelError` | `ErrorLevel` | `LevelError` |

A logger can travel in the context:

```go
ctx = pry.ContextWithLogger(ctx, l)
pry.LoggerFromContext(ctx).Info("started") // pry.Noop() when the context holds none
```

### Options

Options are applied per log call:

| Option | Description |
|--------|-------------|
| `pry.Err(err)` | Attaches an error to the event. |
| `pry.Field(key, value)` | Adds a structured key/value field. |
| `pry.Ctx(ctx)` | Attaches request context (session metadata). See [Session context](#session-context). |

### Logger options

| Option | Description |
|--------|-------------|
| `pry.ToSinks(s...)` | Adds side channels (e.g., Sentry). |

Output format and caller belong to the driver, not the logger:

| Option | Description |
|--------|-------------|
| `pryzerolog.Plain()` | Console writer. Colour is dropped when stdout is not a TTY. |
| `pryzerolog.Json()` | Stdout as JSON. Sets `zerolog.TimestampFieldName` globally, which affects every zerolog logger in the process. |
| `pryzerolog.WithCaller()` | Adds caller file and line to every event. |

## Sinks

A sink receives every record the logger emits, off the calling goroutine.

```go
type Sink interface {
    Write(Record)
    Close() error
}
```

```go
l := pry.New(level, pryzerolog.New(pryzerolog.Plain()), pry.ToSinks(sentry))
// ...
_ = l.Close() // drain every sink before shutdown
```

A sink sees only what the logger emits: the level gate runs first, so a sink
cannot ask for records below the logger's own level.

It can ask for less, by answering the optional half of the interface:

```go
type Leveled interface {
    Level() Level
}
```

A record below that level is never sent, so the queue carries only what the sink
would keep — `channels.Sentry("error")` under `LOGLEVEL=trace` otherwise queues
every request the service serves in order to discard each one on arrival. What a
sink declines is not counted as dropped: it was never wanted.

The direction is one-way on purpose. A sink that could ask for *more* than the
logger emits would be a second place deciding the level, and then neither
`LOGLEVEL` nor the sink answers "what is written" on its own.

### One goroutine per sink, and a queue in front of it

Each sink gets a goroutine and a buffered channel, and `Write` is called from
that goroutine alone. Two things follow, and both are the point:

**Records arrive in order.** A goroutine per record cannot promise that, and the
arrangement that preceded this one started one per record per sink.

**A slow sink costs records, not the process.** The send never blocks — logging
must not become a source of latency — so a full queue drops the record and counts
it. `Dropped()` reads the count, and `Close` writes a line through the
driver when it is not zero, because a drop nobody is told about is the one
failure this design has. It is not an error: a shutdown that panicked over a lost
log line would be worse than the lost line.

Measured on an M2 Max, the fan-out alone:

| | per record |
|---|---|
| a goroutine per record | 327 ns, 1 alloc |
| a send to one goroutine | 11 ns, 0 allocs |

The queue is what bounds a slow sink. Without it the records became goroutines,
and the only limit was memory.

### Sentry / Glitchtip

```go
import "github.com/ribora/pry/channels"

sentry, err := channels.Sentry("error",
    channels.WithSentryConnection(dsn, "production",
        channels.WithSentryService("my-service"),
    ),
)

l := pry.New(pry.LevelDebug, pryzerolog.New(pryzerolog.Plain()), pry.ToSinks(sentry))
```

`Sentry(level, ...)` — only events at or above `level` are forwarded to Sentry.

`WithSentryConnection(dsn, env, opts...)` — configures the Sentry client. If `dsn` is empty the channel is created but silently does nothing.

`WithSentryService(name)` — sets `ServerName` in the Sentry client options.

`WithSentryRelease(release)` — sets `Release`.

`WithSentryScrubber(fn)` — runs `fn` as `BeforeSend` on every event; the event it returns is sent, and `nil` drops it.

When an error is a `failure.Error`, the Sentry event includes the structured stack trace and type code from `failure`. Any other error goes through `failure.Parse` as the sink writes it, so its stack is captured on the sink's goroutine, not at the call site.

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
| `Method` | `request.method`, `tags["method"]` |
| `Body` | `request.data` |
| `UserAgent` | `request.headers["User-Agent"]` |
| `UserLanguage` | `request.headers["Accept-Language"]` |
| `SourceIp` | `user.ip_address` |

`request` is set when `Uri` or `Method` is non-empty. If the context carries no
session, none of these are set. The trace context is read from the context
rather than the session, so `trace_id` and the Sentry propagation context are
still attached to a session-less record.

## Closing

`Close` and `Dropped` are not `Logger` methods — call them on what `New` returned:

```go
l := pry.New(pry.LevelInfo, pryzerolog.New(pryzerolog.Plain()), pry.ToSinks(sentry))
defer func() { _ = l.Close() }()
```

It blocks until the in-flight sink writes finish, then closes each sink (Sentry flushes with a 3-second timeout) and the driver, if the driver is an `io.Closer`.

## Testing

`pry.Recorder()` returns a `Logger` that keeps what it is given:

```go
rec := pry.Recorder()
svc := newService(rec) // takes a pry.Logger

svc.Pay(ctx, order)

errs := rec.At(pry.LevelError)
errs[0].Field("order") // a pry.Field the code attached: value, found
```

It is a driver under the real logger, so the options are applied by the code
production runs and a test asserts on records the production code built.
`Entries()` returns every record, `At(level)` those of one level, `Reset()`
clears them. The recorder is open at trace.

`pry.Noop()` discards everything; use it for a test that asserts nothing about
logging.
