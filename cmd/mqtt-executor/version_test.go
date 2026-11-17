package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVersionString(t *testing.T) {
	version := versionString()

	assert.True(t, strings.HasPrefix(version, "mqtt-executor"), "version string should start with the binary name, got %q", version)
	assert.NotContains(t, version, "\n")
}
