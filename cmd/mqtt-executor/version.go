package main

import (
	"fmt"
	"runtime/debug"
)

// versionTag can be overridden at build time, e.g. in Docker builds where
// there is no VCS info available:
//
//	go build -ldflags "-X main.versionTag=v1.2.3"
var versionTag = ""

// versionString assembles the application version: an injected version tag
// (Docker builds) takes precedence, otherwise the build info is used (module
// version and, if available, the VCS revision).
func versionString() string {
	name := "mqtt-executor"

	if versionTag != "" {
		return fmt.Sprintf("%s %s", name, versionTag)
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return fmt.Sprintf("%s (unknown version)", name)
	}

	version := info.Main.Version
	if version == "" {
		version = "(devel)"
	}
	out := fmt.Sprintf("%s %s", name, version)

	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" && len(setting.Value) >= 7 {
			out += fmt.Sprintf(" (%s)", setting.Value[:7])
		}
	}
	return out
}
