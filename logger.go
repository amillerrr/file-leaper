package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Log levels
const (
	LevelDebug = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

var levelNames = map[int]string{
	LevelDebug: "DEBUG",
	LevelInfo:  "INFO",
	LevelWarn:  "WARN",
	LevelError: "ERROR",
	LevelFatal: "FATAL",
}

// Logger instance
type Logger struct {
	level    int
	logger   *log.Logger
	logFile  *os.File
	logToTty bool
}

var defaultLogger *Logger

// InitLogger initializes the default logger
func InitLogger(level int, logToTty bool) error {
	// Get user's home directory
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %v", err)
	}

	// Create log directory
	logDir := filepath.Join(home, ".file-leaper", "logs")
	err = os.MkdirAll(logDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create log directory: %v", err)
	}

	// Create log file with timestamp
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	logPath := filepath.Join(logDir, fmt.Sprintf("file-leaper_%s.log", timestamp))

	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("failed to create log file: %v", err)
	}

	// Set up multi-writer if logging to TTY
	var writer io.Writer
	if logToTty {
		writer = io.MultiWriter(logFile, os.Stderr)
	} else {
		writer = logFile
	}

	// Create logger
	logger := log.New(writer, "", log.Ldate|log.Ltime)

	defaultLogger = &Logger{
		level:    level,
		logger:   logger,
		logFile:  logFile,
		logToTty: logToTty,
	}

	// Log startup message
	Info("Logger initialized at %s", logPath)

	return nil
}

// CloseLogger closes the logger file
func CloseLogger() {
	if defaultLogger != nil && defaultLogger.logFile != nil {
		defaultLogger.logFile.Close()
	}
}

// Internal log function with level and caller info
func (l *Logger) log(level int, format string, args ...interface{}) {
	if level < l.level {
		return
	}

	// Get caller information
	_, file, line, ok := runtime.Caller(2)
	caller := "unknown"
	if ok {
		// Extract just the filename without the path
		caller = fmt.Sprintf("%s:%d", filepath.Base(file), line)
	}

	// Format message with level and caller
	message := fmt.Sprintf("[%s] [%s] %s", levelNames[level], caller, fmt.Sprintf(format, args...))

	l.logger.Println(message)

	// For fatal logs, exit the program
	if level == LevelFatal {
		os.Exit(1)
	}
}

// Debug logs a debug message
func Debug(format string, args ...interface{}) {
	if defaultLogger != nil {
		defaultLogger.log(LevelDebug, format, args...)
	}
}

// Info logs an informational message
func Info(format string, args ...interface{}) {
	if defaultLogger != nil {
		defaultLogger.log(LevelInfo, format, args...)
	}
}

// Warn logs a warning message
func Warn(format string, args ...interface{}) {
	if defaultLogger != nil {
		defaultLogger.log(LevelWarn, format, args...)
	}
}

// Error logs an error message
func Error(format string, args ...interface{}) {
	if defaultLogger != nil {
		defaultLogger.log(LevelError, format, args...)
	}
}

// Fatal logs a fatal error message and exits the program
func Fatal(format string, args ...interface{}) {
	if defaultLogger != nil {
		defaultLogger.log(LevelFatal, format, args...)
	}
	// Ensure we exit even if logger isn't initialized
	log.Fatalf(format, args...)
}
