// Package app contains the ai-mr-comment CLI application.
package app

import (
	"fmt"
	"strings"
	"sync"
)

// Version is set by cmd/ai-mr-comment from build-time metadata.
var Version = "dev"

// Commit is the short commit SHA set by cmd/ai-mr-comment from build-time metadata.
var Commit = "unknown"

// CommitFull is the full commit SHA set by cmd/ai-mr-comment from build-time metadata.
var CommitFull = "unknown"

// SetBuildInfo applies build metadata collected by the executable entry point.
func SetBuildInfo(version, commit, commitFull string) {
	Version = version
	Commit = commit
	CommitFull = commitFull
}

var debugWriterMu sync.Mutex

// Execute runs the ai-mr-comment CLI.
func Execute() error {
	return newRootCmd(chatCompletions).Execute()
}

type exitCodeError int

func (e exitCodeError) Error() string {
	return fmt.Sprintf("exit code %d", int(e))
}

func (e exitCodeError) ExitCode() int {
	return int(e)
}

// SilentExit marks verdict failures, whose result has already been rendered,
// so the executable can return the requested status without printing a second
// generic error line.
func (e exitCodeError) SilentExit() bool {
	return true
}

type codedError struct {
	code int
	err  error
}

func (e codedError) Error() string {
	return e.err.Error()
}

func (e codedError) Unwrap() error {
	return e.err
}

func (e codedError) ExitCode() int {
	return e.code
}

func withExitCode(code int, err error) error {
	if err == nil {
		return nil
	}
	return codedError{code: code, err: err}
}

// parseVerdict extracts the VERDICT line prepended by the AI when --exit-code is
// active. Returns the verdict token ("PASS", "FAIL", or "UNKNOWN") and the body
// with the verdict line stripped. "UNKNOWN" indicates a missing/invalid verdict
// line and should be handled as fail-closed by callers.
func parseVerdict(comment string) (verdict, body string) {
	if verdictComment, ok := strings.CutPrefix(comment, "VERDICT: "); ok {
		lines := strings.SplitN(verdictComment, "\n", 2)
		verdict = strings.TrimSpace(lines[0])
		if len(lines) > 1 {
			body = strings.TrimSpace(lines[1])
		}
		return verdict, body
	}
	return "UNKNOWN", comment
}
