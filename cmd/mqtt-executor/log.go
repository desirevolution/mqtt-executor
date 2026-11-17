package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	MQTT "github.com/eclipse/paho.mqtt.golang"
)

func init() {
	//initialise a default logger (text) before the flags are parsed; the
	//final logger incl. the configured format is set up in setupLogging
	//right after flag.Parse
	_ = setupLogging("text", os.Stderr)
}

// setupLogging builds the application logger in the requested format and
// routes the paho internal loggers into it. "text" and "json" are supported.
func setupLogging(format string, out io.Writer) error {
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}

	var handler slog.Handler
	switch format {
	case "", "text":
		handler = slog.NewTextHandler(out, opts)
	case "json":
		handler = slog.NewJSONHandler(out, opts)
	default:
		return fmt.Errorf("invalid log format %q: use one of text, json", format)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)

	//route the paho internal loggers into our logger
	MQTT.ERROR = slog.NewLogLogger(handler, slog.LevelError)
	MQTT.CRITICAL = slog.NewLogLogger(handler, slog.LevelError)
	MQTT.WARN = slog.NewLogLogger(handler, slog.LevelWarn)
	MQTT.DEBUG = slog.NewLogLogger(handler, slog.LevelDebug)
	return nil
}

// fatal logs the message on error level and terminates the process.
// slog has no Fatal level, so we emulate zap's behaviour here.
func fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}
