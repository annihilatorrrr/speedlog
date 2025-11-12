package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Log levels
const (
	DEBUG = iota
	INFO
	WARN
	ERROR
)

var levelNames = [4]string{"DEBUG", "INFO", "WARN", "ERROR"}

type Logger struct {
	mu      sync.Mutex
	level   int
	writers []io.Writer
	bufPool sync.Pool
	bufs    []*bufio.Writer
}

var std *Logger

func init() {
	std = &Logger{
		level:   INFO,
		writers: []io.Writer{os.Stdout},
		bufPool: sync.Pool{
			New: func() interface{} {
				return make([]byte, 0, 256)
			},
		},
	}
	std.bufs = []*bufio.Writer{bufio.NewWriterSize(os.Stdout, 4096)}
}

// New creates a new logger
// If filePath is empty (""), logs only to terminal
// If filePath is provided, logs to both file and terminal
func New(filePath string) (*Logger, error) {
	l := &Logger{
		level: INFO,
		bufPool: sync.Pool{
			New: func() interface{} {
				return make([]byte, 0, 256)
			},
		},
	}

	// Terminal only
	if filePath == "" {
		l.writers = []io.Writer{os.Stdout}
		l.bufs = []*bufio.Writer{bufio.NewWriterSize(os.Stdout, 4096)}
		return l, nil
	}

	// File + Terminal
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}

	l.writers = []io.Writer{os.Stdout, file}
	l.bufs = []*bufio.Writer{
		bufio.NewWriterSize(os.Stdout, 4096),
		bufio.NewWriterSize(file, 4096),
	}

	return l, nil
}

func (l *Logger) autoFlush(interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for range t.C {
			l.mu.Lock()
			for _, bw := range l.bufs {
				bw.Flush()
			}
			l.mu.Unlock()
		}
	}()
}

// SetLevel sets minimum log level
func SetLevel(level int) {
	std.mu.Lock()
	std.level = level
	std.mu.Unlock()
}

// SetLevelForLogger sets level for specific logger instance
func (l *Logger) SetLevel(level int) {
	l.mu.Lock()
	l.level = level
	l.mu.Unlock()
}

// Close flushes and closes the logger
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, buf := range l.bufs {
		buf.Flush()
	}

	for _, w := range l.writers {
		if c, ok := w.(io.Closer); ok {
			if err := c.Close(); err != nil {
				return err
			}
		}
	}
	return nil
}

// Close closes the default logger
func Close() error {
	return std.Close()
}

// Internal log function (optimized)
func (l *Logger) log(level int, msg string) {
	if level < l.level {
		return
	}

	buf := l.bufPool.Get().([]byte)[:0]
	buf = append(buf, time.Now().Format("2006-01-02 15:04:05")...)
	buf = append(buf, " ["...)
	buf = append(buf, levelNames[level]...)
	buf = append(buf, "] "...)
	buf = append(buf, msg...)
	buf = append(buf, '\n')

	l.mu.Lock()
	for _, bw := range l.bufs {
		bw.Write(buf)
	}
	for _, bw := range l.bufs {
		bw.Flush()
	}
	l.mu.Unlock()

	l.bufPool.Put(buf)
}

// Internal formatted log function
func (l *Logger) logf(level int, format string, args ...interface{}) {
	if level < l.level {
		return
	}

	buf := l.bufPool.Get().([]byte)[:0]
	buf = append(buf, time.Now().Format("2006-01-02 15:04:05")...)
	buf = append(buf, " ["...)
	buf = append(buf, levelNames[level]...)
	buf = append(buf, "] "...)
	buf = fmt.Appendf(buf, format, args...)
	buf = append(buf, '\n')

	l.mu.Lock()
	for _, bw := range l.bufs {
		bw.Write(buf)
	}
	for _, bw := range l.bufs {
		bw.Flush()
	}
	l.mu.Unlock()

	l.bufPool.Put(buf)
}

// Debug logs debug message
func Debug(msg string) {
	std.log(DEBUG, msg)
}

// Debugf logs formatted debug message
func Debugf(format string, args ...interface{}) {
	std.logf(DEBUG, format, args...)
}

// Print logs info message (normal logging)
func Print(msg string) {
	std.log(INFO, msg)
}

// Printf logs formatted info message
func Printf(format string, args ...interface{}) {
	std.logf(INFO, format, args...)
}

// Warn logs warning message
func Warn(msg string) {
	std.log(WARN, msg)
}

// Warnf logs formatted warning message
func Warnf(format string, args ...interface{}) {
	std.logf(WARN, format, args...)
}

// Error logs error message
func Error(msg string) {
	std.log(ERROR, msg)
}

// Errorf logs formatted error message
func Errorf(format string, args ...interface{}) {
	std.logf(ERROR, format, args...)
}

// Instance methods
func (l *Logger) Debug(msg string) {
	l.log(DEBUG, msg)
}

func (l *Logger) Debugf(format string, args ...interface{}) {
	l.logf(DEBUG, format, args...)
}

func (l *Logger) Print(msg string) {
	l.log(INFO, msg)
}

func (l *Logger) Printf(format string, args ...interface{}) {
	l.logf(INFO, format, args...)
}

func (l *Logger) Warn(msg string) {
	l.log(WARN, msg)
}

func (l *Logger) Warnf(format string, args ...interface{}) {
	l.logf(WARN, format, args...)
}

func (l *Logger) Error(msg string) {
	l.log(ERROR, msg)
}

func (l *Logger) Errorf(format string, args ...interface{}) {
	l.logf(ERROR, format, args...)
}
