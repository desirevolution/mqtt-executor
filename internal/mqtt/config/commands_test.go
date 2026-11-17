package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateCommands(t *testing.T) {
	//non-executable file
	notExecutable := filepath.Join(t.TempDir(), "not-executable")
	assert.NoError(t, os.WriteFile(notExecutable, []byte("#!/bin/sh\n"), 0o644))

	//executable file
	executable := filepath.Join(t.TempDir(), "executable")
	assert.NoError(t, os.WriteFile(executable, []byte("#!/bin/sh\n"), 0o755))

	tests := []struct {
		name        string
		commands    []string
		expectError bool
		errorPart   string
	}{
		{name: "bare name in PATH", commands: []string{"echo"}, expectError: false},
		{name: "existing absolute path", commands: []string{"/bin/sh"}, expectError: false},
		{name: "existing executable file", commands: []string{executable}, expectError: false},
		{name: "missing absolute path", commands: []string{"/nonexistent/binary-xyz"}, expectError: true, errorPart: "not found"},
		{name: "bare name not in PATH", commands: []string{"definitely-not-a-command-xyz"}, expectError: true, errorPart: "not found in PATH"},
		{name: "directory", commands: []string{"/tmp"}, expectError: true, errorPart: "is a directory"},
		{name: "not executable", commands: []string{notExecutable}, expectError: true, errorPart: "is not executable"},
		{name: "no commands at all", commands: []string{}, expectError: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := TopicConfigurations{}
			for _, cmd := range tt.commands {
				tc.Trigger = append(tc.Trigger, Trigger{Name: cmd, Topic: "cmnd/" + cmd, Command: Command{Name: cmd}})
			}

			err := tc.ValidateCommands()
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorPart)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateCommandsFindsAllEntityTypes(t *testing.T) {
	//a missing command must be found in triggers, sensors AND multi sensors
	tc := TopicConfigurations{
		Trigger: []Trigger{{
			Name: "t", Topic: "cmnd/t",
			Command: Command{Name: "definitely-not-a-command-xyz"},
		}},
		Sensor: []Sensor{{
			Name:          "s",
			GeneralSensor: GeneralSensor{ResultTopic: "tele/s", Interval: Interval(1), Command: Command{Name: "echo"}},
		}},
		MultiSensor: []MultiSensor{{
			GeneralSensor: GeneralSensor{ResultTopic: "tele/ms", Interval: Interval(1), Command: Command{Name: "echo"}},
			Values:        []MultiSensorValue{{Name: "v", Template: "{{value}}"}},
		}},
	}

	err := tc.ValidateCommands()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "definitely-not-a-command-xyz")

	//now the same config but the trigger command exists
	tc.Trigger[0].Command.Name = "echo"
	assert.NoError(t, tc.ValidateCommands())
}
