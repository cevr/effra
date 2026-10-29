package build

import (
	"slices"
	"testing"
)

// TestCommandKeepsTheUsersGoConfig: the Go build action passes the user's
// HOME, XDG_CONFIG_HOME, GOENV, AppData and GOTOOLCHAIN through unchanged,
// so the go command reads the user's own go env file and keeps its
// telemetry and other state where the user's go command does (the
// telemetry directory follows os.UserConfigDir and cannot be set). Only
// GOWORK, GOOS, GOARCH and GOFLAGS are the action's own.
func TestCommandKeepsTheUsersGoConfig(t *testing.T) {
	user := map[string]string{
		"HOME": "/users/me", "XDG_CONFIG_HOME": "/users/me/config", "GOENV": "/users/me/config/go/env",
		"AppData": `C:\Users\me\AppData\Roaming`, "GOTOOLCHAIN": "local",
	}
	for name, value := range user {
		t.Setenv(name, value)
	}
	t.Setenv("GOWORK", "/somewhere/go.work")
	command := Toolchain{Go: "go", GOOS: "linux"}.Command(Module{Dir: "/cache/modules/key"}, "/out/app")
	for name, value := range user {
		if !slices.Contains(command.Env, name+"="+value) {
			t.Errorf("the action dropped or changed %s=%s", name, value)
		}
	}
	if !slices.Contains(command.Env, "GOWORK=off") || slices.Contains(command.Env, "GOWORK=/somewhere/go.work") || !slices.Contains(command.Env, "GOOS=linux") {
		t.Errorf("the action's own variables are wrong: %v", command.Env)
	}
}
