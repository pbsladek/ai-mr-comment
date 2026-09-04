// Package main is the executable entry point for ai-mr-comment.
package main

import (
	"errors"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/pbsladek/ai-mr-comment/internal/app"
)

// Version is set at build time via -ldflags "-X 'main.Version=...'".
// It falls back to VCS info embedded by the Go toolchain.
var Version = "dev"

// Commit is the short commit SHA, set at build time via -ldflags "-X 'main.Commit=...'".
// It falls back to VCS info embedded by the Go toolchain.
var Commit = "unknown"

// CommitFull is the full commit SHA, set at build time via -ldflags "-X 'main.CommitFull=...'".
// It falls back to VCS info embedded by the Go toolchain.
var CommitFull = "unknown"

// Date is accepted for GoReleaser compatibility.
var Date = ""

// BuiltBy is accepted for GoReleaser compatibility.
var BuiltBy = ""

type exitCoder interface {
	error
	ExitCode() int
}

type silentExit interface {
	error
	SilentExit() bool
}

func init() {
	if Version != "dev" || Commit != "unknown" {
		return
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				if len(s.Value) >= 7 {
					CommitFull = s.Value
					Commit = s.Value[:7]
				}
			case "vcs.version":
				if s.Value != "" {
					Version = s.Value
				}
			}
		}
	}
}

func main() {
	app.SetBuildInfo(Version, Commit, CommitFull)
	if err := app.Execute(); err != nil {
		if exitErr, ok := errors.AsType[exitCoder](err); ok {
			silent, isSilent := errors.AsType[silentExit](err)
			if !isSilent || !silent.SilentExit() {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			}
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
