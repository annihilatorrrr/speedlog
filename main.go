package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// ---------- LEVELS ----------
const (
	DEBUG = iota
	INFO
	WARN
	ERROR
)

var levelNames = [4]string{"DEBUG", "INFO", "WARN", "ERROR"}

// ---------- LOGGER STRUCT ----------
type Logger struct {
	level   atomic.Int32
	writers []io.Writer
	bufs    []*bufio.Writer
	ch      chan []byte
	done    chan struct{}
	wg      sync.WaitGroup
	bufPool sync.Pool
}

// ---------- DEFAULT INSTANCE ----------
var std *Logger

func init() {
	l, _ := New("")
	std = l
}

// ---------- NEW ----------
func New(filePath string) (*Logger, error) {
	l := &Logger{
		ch:   make(chan []byte, 4096),
		done: make(chan struct{}),
		bufPool: sync.Pool{
			New: func() interface{} {
				b := make([]byte, 0, 256)
				return &b
			},
		},
	}
	l.level.Store(int32(INFO))

	if filePath == "" {
		l.writers = []io.Writer{os.Stdout}
		l.bufs = []*bufio.Writer{bufio.NewWriterSize(os.Stdout, 4096)}
	} else {
		file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, err
		}
		l.writers = []io.Writer{os.Stdout, file}
		l.bufs = []*bufio.Writer{
			bufio.NewWriterSize(os.Stdout, 4096),
			bufio.NewWriterSize(file, 4096),
		}
	}

	l.wg.Add(2)
	go l.runWriter()
	go l.autoFlush(2 * time.Second)
	return l, nil
}

// ---------- WRITER LOOP ----------
func (l *Logger) runWriter() {
	defer l.wg.Done()
	for {
		select {
		case buf := <-l.ch:
			for _, bw := range l.bufs {
				bw.Write(buf)
			}
		case <-l.done:
			// Drain remaining messages
			for {
				select {
				case buf := <-l.ch:
					for _, bw := range l.bufs {
						bw.Write(buf)
					}
				default:
					for _, bw := range l.bufs {
						bw.Flush()
					}
					return
				}
			}
		}
	}
}

func (l *Logger) autoFlush(interval time.Duration) {
	defer l.wg.Done()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-l.done:
			return
		case <-t.C:
			for _, bw := range l.bufs {
				bw.Flush()
			}
		}
	}
}

// ---------- CORE LOGGING ----------
func (l *Logger) log(level int, msg string) {
	if level < int(l.level.Load()) {
		return
	}
	ts := time.Now()
	bufPtr := l.bufPool.Get().(*[]byte)
	buf := (*bufPtr)[:0]
	
	buf = ts.AppendFormat(buf, "2006-01-02 15:04:05")
	buf = append(buf, " ["...)
	buf = append(buf, levelNames[level]...)
	buf = append(buf, "] "...)
	buf = append(buf, msg...)
	buf = append(buf, '\n')

	// Create a copy for the channel
	bufCopy := make([]byte, len(buf))
	copy(bufCopy, buf)
	
	// Return buffer to pool
	*bufPtr = buf
	l.bufPool.Put(bufPtr)

	// Non-blocking send with fallback
	select {
	case l.ch <- bufCopy:
	case <-l.done:
		// Logger is closing, drop message
	default:
		// Channel full, drop message (or could block here if preferred)
		// For production, you might want to increment a dropped message counter
	}
}

func (l *Logger) logf(level int, format string, args ...interface{}) {
	if level < int(l.level.Load()) {
		return
	}
	ts := time.Now()
	bufPtr := l.bufPool.Get().(*[]byte)
	buf := (*bufPtr)[:0]
	
	buf = ts.AppendFormat(buf, "2006-01-02 15:04:05")
	buf = append(buf, " ["...)
	buf = append(buf, levelNames[level]...)
	buf = append(buf, "] "...)
	buf = fmt.Appendf(buf, format, args...)
	buf = append(buf, '\n')

	// Create a copy for the channel
	bufCopy := make([]byte, len(buf))
	copy(bufCopy, buf)
	
	// Return buffer to pool
	*bufPtr = buf
	l.bufPool.Put(bufPtr)

	select {
	case l.ch <- bufCopy:
	case <-l.done:
	default:
		// Channel full, drop message
	}
}

// ---------- CONTROL ----------
func (l *Logger) SetLevel(level int) {
	if level < DEBUG || level > ERROR {
		return
	}
	l.level.Store(int32(level))
}

func (l *Logger) GetLevel() int {
	return int(l.level.Load())
}

func (l *Logger) IsLevelEnabled(level int) bool {
	return level >= int(l.level.Load())
}

func (l *Logger) Sync() {
	for _, bw := range l.bufs {
		bw.Flush()
	}
}

func (l *Logger) Close() {
	close(l.done)
	l.wg.Wait() // Wait for goroutines to finish
	
	for _, bw := range l.bufs {
		bw.Flush()
	}
	for _, w := range l.writers {
		if c, ok := w.(io.Closer); ok {
			c.Close()
		}
	}
}

// ---------- GLOBAL WRAPPERS ----------
func SetLevel(level int)    { std.SetLevel(level) }
func GetLevel() int         { return std.GetLevel() }
func IsLevelEnabled(level int) bool { return std.IsLevelEnabled(level) }
func Close()                { std.Close() }
func Sync()                 { std.Sync() }

func Debug(msg string)                          { std.log(DEBUG, msg) }
func Debugf(format string, args ...interface{}) { std.logf(DEBUG, format, args...) }
func Print(msg string)                          { std.log(INFO, msg) }
func Printf(format string, args ...interface{}) { std.logf(INFO, format, args...) }
func Warn(msg string)                           { std.log(WARN, msg) }
func Warnf(format string, args ...interface{})  { std.logf(WARN, format, args...) }
func Error(msg string)                          { std.log(ERROR, msg) }
func Errorf(format string, args ...interface{}) { std.logf(ERROR, format, args...) }

// ---------- INSTANCE METHODS ----------
func (l *Logger) Debug(msg string)                          { l.log(DEBUG, msg) }
func (l *Logger) Debugf(format string, args ...interface{}) { l.logf(DEBUG, format, args...) }
func (l *Logger) Print(msg string)                          { l.log(INFO, msg) }
func (l *Logger) Printf(format string, args ...interface{}) { l.logf(INFO, format, args...) }
func (l *Logger) Warn(msg string)                           { l.log(WARN, msg) }
func (l *Logger) Warnf(format string, args ...interface{})  { l.logf(WARN, format, args...) }
func (l *Logger) Error(msg string)                          { l.log(ERROR, msg) }
func (l *Logger) Errorf(format string, args ...interface{}) { l.logf(ERROR, format, args...) }
