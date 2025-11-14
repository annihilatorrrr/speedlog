# speedlog (async buffered logger for Go)

High-throughput, low-GC logger for Go services.

- Async logging with a background writer goroutine
- `bufio.Writer` + `sync.Pool` for low allocations
- Cached timestamp (no `time.Now().Format` on every log)
- No log drops while running (channel backpressure instead)
- Global logger + per-instance loggers
- API stays simple: `Debug/Print/Warn/Error` + `*f` variants

This is for **servers, bots, workers, daemons** that actually log under load.

---

## When to use this

**Good fit:**

- Telegram bots, HTTP APIs, gRPC services, workers, cron daemons
- Apps that log **a lot** or have **many goroutines** logging
- You care about **contention**, **latency**, and not losing logs

**Overkill / “just use std log”:**

- Tiny CLI tools that log only at startup/shutdown
- Apps that log `< 1 line / second` most of the time
- Super tight memory environments (microcontrollers etc.)

For those cases, you can either:
- Just use `log.Println`, or
- Add a “sync mode” wrapper around this and skip the goroutines.

---

## Install

Assuming your module path is:

```bash
go get github.com/annihilatorrrr/speedlog
````

Then:

```go
import "github.com/annihilatorrrr/speedlog"
```

Adjust the path to whatever you actually publish (whenever you copy the file).

---

## API Overview

### Levels

```go
const (
    DEBUG = iota
    INFO
    WARN
    ERROR
)
```

### Global logger

Automatically initialized with:

* Level: `INFO`
* Writer: `os.Stdout`
* Channel size: `1024`

Global helpers:

```go
speedlog.SetLevel(speedlog.DEBUG)
speedlog.GetLevel() int
speedlog.IsLevelEnabled(level int) bool

speedlog.Sync()  // best-effort flush
speedlog.Close() // clean shutdown, flush + close writers
```

Logging:

```go
speedlog.Debug("debug msg")
speedlog.Debugf("debug %d", n)

speedlog.Print("info msg")
speedlog.Printf("info %s", s)

speedlog.Warn("warn msg")
speedlog.Warnf("warn: %v", err)

speedlog.Error("error msg")
speedlog.Errorf("error: %v", err)
```

### Creating your own logger instance

Options:

```go
func New(opts ...Option) *Logger

type Option func(*Logger)

func WithWriter(w io.Writer) Option
func WithChannelSize(n int) Option       // default: 1024
func WithLevel(level int) Option         // default: INFO
```

Instance methods:

```go
l.SetLevel(level int)
l.GetLevel() int
l.IsLevelEnabled(level int) bool

l.Sync()
l.Close()  // idempotent

l.Debug(msg string)
l.Debugf(format string, args ...any)
l.Print(msg string)
l.Printf(format string, args ...any)
l.Warn(msg string)
l.Warnf(format string, args ...any)
l.Error(msg string)
l.Errorf(format string, args ...any)
```

---

## Behavior & Guarantees

* **No dropped logs while running**

  * If the channel is full, callers block until space is available.
  * If `Close()` has been called, new logs are discarded after freeing the buffer.

* **Shutdown (`Close`)**

  * Signals both internal goroutines to stop.
  * Drains remaining logs from the channel.
  * Flushes all `bufio.Writer`s.
  * Closes underlying `io.Closer`s (e.g., files).
  * Safe to call multiple times (uses `sync.Once`).

* **Sync (`Sync`)**

  * Best-effort flush of `bufio.Writer`s.
  * Writer goroutine is still running; anything already in the channel will be written and flushed either on the next tick or next `Sync` call.
  * Use this if you want logs flushed before a risky operation.

* **Timestamps**

  * Cached formatted timestamp updated every `100ms` by a background goroutine.
  * Hot path just reads a `[]byte` via `atomic.Value` and appends it – no `time.Format` per log.

* **Flushing**

  * Writer goroutine flushes all writers every `500ms` via ticker.
  * Also flushes once at shutdown after draining the channel.

---

## Example: using the global logger

```go
package main

import (
    "net/http"
    "time"

    "github.com/annihilatorrrr/speedlog"
)

func main() {
    // Make sure we flush + shutdown cleanly on exit.
    defer speedlog.Close()

    speedlog.SetLevel(speedlog.DEBUG)

    speedlog.Print("service starting up")

    mux := http.NewServeMux()
    mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        w.Write([]byte("pong\n"))
        dur := time.Since(start)

        speedlog.Debugf("handled /ping in %s from %s", dur, r.RemoteAddr)
    })

    srv := &http.Server{
        Addr:    ":8080",
        Handler: mux,
    }

    speedlog.Print("listening on :8080")

    if err := srv.ListenAndServe(); err != nil {
        speedlog.Errorf("server error: %v", err)
    }

    speedlog.Print("service shutting down")
}
```

---

## Example: custom instance with file + stdout

```go
package main

import (
    "log"
    "os"

    "github.com/annihilatorrrr/speedlog"
)

func main() {
    f, err := os.OpenFile("app.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
    if err != nil {
        // we don't have logger yet, so just die loud
        log.Fatalf("failed to open log file: %v", err)
    }
    defer f.Close()

    logger := speedlog.New(
        speedlog.WithWriter(os.Stdout),
        speedlog.WithWriter(f),
        speedlog.WithChannelSize(4096),
        speedlog.WithLevel(speedlog.DEBUG),
    )
    defer logger.Close()

    logger.Print("app started")
    logger.Debug("debug details here")

    // simulated work
    for i := 0; i < 10; i++ {
        logger.Debugf("processing item %d", i)
    }

    logger.Warn("about to exit")
}
```

---

## Gotchas / notes

* If you spam `Errorf` with heavy formatting in a tight loop, the bottleneck is `fmt.Sprintf`, not the logger.
* Channel backpressure means your app **can** slow down if you out-log your IO sink. That’s intentional: better slow than silently lose logs.
* If you really need non-blocking logs with drops, you can change the send logic to `select` + `default` and discard on full – but then you’re in “zap `SampledLogger`” territory and should document that clearly.

### `example/main.go`

Here’s a clean `main.go` you can slap into an `example/` folder in the repo:

```go
package main

import (
    "net/http"
    "os"
    "time"

    "github.com/annihilatorrrr/speedlog"
)

func main() {
    // Combined stdout + file logger instance
    logFile, err := os.OpenFile("server.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
    if err != nil {
        // fall back to global logger
        speedlog.Errorf("failed to open log file: %v", err)
        return
    }
    defer logFile.Close()

    logger := speedlog.New(
        speedlog.WithWriter(os.Stdout),
        speedlog.WithWriter(logFile),
        speedlog.WithChannelSize(4096),
        speedlog.WithLevel(speedlog.DEBUG),
    )
    defer logger.Close()

    logger.Print("http server starting")

    mux := http.NewServeMux()
    mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        _, _ = w.Write([]byte("pong\n"))
        dur := time.Since(start)

        if logger.IsLevelEnabled(speedlog.DEBUG) {
            logger.Debugf("path=/ping remote=%s duration=%s", r.RemoteAddr, dur)
        }
    })

    srv := &http.Server{
        Addr:    ":8080",
        Handler: mux,
    }

    logger.Printf("listening on %s", srv.Addr)

    if err := srv.ListenAndServe(); err != nil {
        logger.Errorf("server error: %v", err)
    }

    logger.Print("http server stopped")
}
```
