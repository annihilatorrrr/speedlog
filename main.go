package speedlog

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

const (
	DEBUG = iota
	INFO
	WARN
	ERROR
)

var (
	levelNames = [...]string{"DEBUG", "INFO", "WARN", "ERROR"}
	std        *Logger
)

type Logger struct {
	level     int32
	writers   []io.Writer
	bufs      []*bufio.Writer
	ch        chan []byte
	bufPool   sync.Pool
	done      chan struct{}
	wg        sync.WaitGroup
	closeOnce sync.Once
	ts        atomic.Value
}

type Option func(*Logger)

func init() {
	std = New(
		WithWriter(os.Stdout),
	)
}

func WithWriter(w io.Writer) Option {
	return func(l *Logger) {
		if w != nil {
			l.writers = append(l.writers, w)
		}
	}
}

func WithChannelSize(n int) Option {
	return func(l *Logger) {
		if n > 0 {
			l.ch = make(chan []byte, n)
		}
	}
}

func WithLevel(level int) Option {
	return func(l *Logger) {
		atomic.StoreInt32(&l.level, int32(level))
	}
}

func New(opts ...Option) *Logger {
	l := &Logger{
		done: make(chan struct{}),
	}
	atomic.StoreInt32(&l.level, int32(INFO))
	l.ch = make(chan []byte, 1024)
	l.bufPool = sync.Pool{
		New: func() interface{} {
			b := make([]byte, 0, 512)
			return b
		},
	}
	for _, opt := range opts {
		opt(l)
	}
	if len(l.writers) == 0 {
		l.writers = []io.Writer{os.Stdout}
	}
	l.bufs = make([]*bufio.Writer, len(l.writers))
	for i, w := range l.writers {
		l.bufs[i] = bufio.NewWriterSize(w, 64*1024)
	}
	now := time.Now()
	ts := make([]byte, 0, 32)
	ts = now.AppendFormat(ts, "2006-01-02 15:04:05.000")
	l.ts.Store(ts)
	l.wg.Add(2)
	go l.writerLoop()
	go l.timestampLoop()
	return l
}

func (l *Logger) writerLoop() {
	defer l.wg.Done()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	flushAll := func() {
		for _, bw := range l.bufs {
			_ = bw.Flush()
		}
	}
	for {
		select {
		case line := <-l.ch:
			if line != nil {
				for _, bw := range l.bufs {
					_, _ = bw.Write(line)
				}
				l.bufPool.Put(line)
			}
		case <-ticker.C:
			flushAll()
		case <-l.done:
			for {
				select {
				case line := <-l.ch:
					if line != nil {
						for _, bw := range l.bufs {
							_, _ = bw.Write(line)
						}
						l.bufPool.Put(line)
					}
				default:
					flushAll()
					return
				}
			}
		}
	}
}

func (l *Logger) timestampLoop() {
	defer l.wg.Done()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			now := time.Now()
			buf := make([]byte, 0, 32)
			buf = now.AppendFormat(buf, "2006-01-02 15:04:05.000")
			l.ts.Store(buf)
		case <-l.done:
			return
		}
	}
}

func (l *Logger) IsLevelEnabled(level int) bool {
	return level >= int(atomic.LoadInt32(&l.level))
}

func (l *Logger) SetLevel(level int) {
	atomic.StoreInt32(&l.level, int32(level))
}

func (l *Logger) GetLevel() int {
	return int(atomic.LoadInt32(&l.level))
}

func (l *Logger) log(level int, msg string) {
	if !l.IsLevelEnabled(level) {
		return
	}
	buf := l.bufPool.Get().([]byte)
	buf = buf[:0]
	ts := l.ts.Load().([]byte)
	buf = append(buf, ts...)
	buf = append(buf, ' ')
	if level >= 0 && level < len(levelNames) {
		buf = append(buf, levelNames[level]...)
	} else {
		buf = append(buf, "UNK"...)
	}
	buf = append(buf, ' ')
	buf = append(buf, msg...)
	buf = append(buf, '\n')
	select {
	case l.ch <- buf:
	case <-l.done:
		l.bufPool.Put(buf)
	}
}

func (l *Logger) logf(level int, format string, args ...interface{}) {
	if !l.IsLevelEnabled(level) {
		return
	}
	msg := fmt.Sprintf(format, args...)
	l.log(level, msg)
}

func (l *Logger) Sync() {
	for _, bw := range l.bufs {
		_ = bw.Flush()
	}
}

func (l *Logger) Close() {
	l.closeOnce.Do(func() {
		close(l.done)
		l.wg.Wait()
		for _, bw := range l.bufs {
			_ = bw.Flush()
		}
		for _, w := range l.writers {
			if c, ok := w.(io.Closer); ok {
				_ = c.Close()
			}
		}
	})
}

func SetLevel(level int) { std.SetLevel(level) }

func GetLevel() int { return std.GetLevel() }

func IsLevelEnabled(level int) bool { return std.IsLevelEnabled(level) }

func Sync() { std.Sync() }

func Close() { std.Close() }

func Debug(msg string) { std.log(DEBUG, msg) }

func Debugf(format string, a ...any) { std.logf(DEBUG, format, a...) }

func Print(msg string) { std.log(INFO, msg) }

func Printf(format string, a ...any) { std.logf(INFO, format, a...) }

func Warn(msg string) { std.log(WARN, msg) }

func Warnf(format string, a ...any) { std.logf(WARN, format, a...) }

func Error(msg string) { std.log(ERROR, msg) }

func Errorf(format string, a ...any) { std.logf(ERROR, format, a...) }

func (l *Logger) Debug(msg string) { l.log(DEBUG, msg) }

func (l *Logger) Debugf(format string, a ...any) { l.logf(DEBUG, format, a...) }

func (l *Logger) Print(msg string) { l.log(INFO, msg) }

func (l *Logger) Printf(format string, a ...any) { l.logf(INFO, format, a...) }

func (l *Logger) Warn(msg string) { l.log(WARN, msg) }

func (l *Logger) Warnf(format string, a ...any) { l.logf(WARN, format, a...) }

func (l *Logger) Error(msg string) { l.log(ERROR, msg) }

func (l *Logger) Errorf(format string, a ...any) { l.logf(ERROR, format, a...) }
