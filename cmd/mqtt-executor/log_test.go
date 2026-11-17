package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupLoggingInvalidFormat(t *testing.T) {
	err := setupLogging("xml", &bytes.Buffer{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid log format")
}

func TestSetupLoggingTextAndJson(t *testing.T) {
	//restore the default text logger on stderr for the other tests in this
	//package (setupLogging mutates the global slog default + paho loggers)
	t.Cleanup(func() {
		require.NoError(t, setupLogging("text", os.Stderr))
	})

	out := &bytes.Buffer{}
	require.NoError(t, setupLogging("json", out))

	slog.Info("test message", "key", "value")

	line := out.String()
	require.True(t, strings.TrimSpace(line) != "", "expected log output")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(line), &parsed), "json format must produce parseable json per line")
	assert.Equal(t, "test message", parsed["msg"])
	assert.Equal(t, "value", parsed["key"])
}
