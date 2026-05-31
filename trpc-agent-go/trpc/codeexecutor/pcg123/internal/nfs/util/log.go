package util

import "log"

// DefaultLogger is the default logger instance used by package-level logging functions.
var DefaultLogger Logger

// Logger defines the interface for logging operations.
type Logger interface {
	SetDebug(bool)
	Errorf(format string, args ...any)
	Debugf(format string, args ...any)
	Infof(format string, args ...any)
}

func init() {
	DefaultLogger = &logger{}
}

type logger struct {
	DebugLevel bool
}

func (l *logger) SetDebug(enable bool) {
	l.DebugLevel = enable
}

func (l *logger) Errorf(format string, args ...any) {
	log.Printf(format, args...)
}

func (l *logger) Debugf(format string, args ...any) {
	if !l.DebugLevel {
		return
	}

	log.Printf(format, args...)
}

func (l *logger) Infof(format string, args ...any) {
	log.Printf(format, args...)
}

// Errorf logs an error message using the default logger.
func Errorf(format string, args ...any) {
	DefaultLogger.Errorf(format, args...)
}

// Debugf logs a debug message using the default logger.
// Debug messages are only logged if debug mode is enabled.
func Debugf(format string, args ...any) {
	DefaultLogger.Debugf(format, args...)
}

// Infof logs an info message using the default logger.
func Infof(format string, args ...any) {
	DefaultLogger.Infof(format, args...)
}
