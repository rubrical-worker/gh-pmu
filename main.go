package main

import (
	"os"

	"github.com/rubrical-works/gh-pmu/cmd"
	"github.com/rubrical-works/gh-pmu/internal/api"
)

func main() {
	os.Exit(exitCode(cmd.Execute()))
}

// exitCode maps a command error to the process exit code: 0 on success,
// api.ExitCodePartialSuccess when a command completed part of its work before a
// later step failed (#929, #938), and 1 for every other failure.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if api.IsPartialSuccess(err) {
		return api.ExitCodePartialSuccess
	}
	return 1
}
