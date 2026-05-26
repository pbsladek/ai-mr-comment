package app

import (
	"errors"
	"testing"
)

func TestBuildInfoAndExitCodeErrors(t *testing.T) {
	oldVersion, oldCommit, oldCommitFull := Version, Commit, CommitFull
	t.Cleanup(func() { SetBuildInfo(oldVersion, oldCommit, oldCommitFull) })

	SetBuildInfo("v1", "abc123", "abc123full")
	if Version != "v1" || Commit != "abc123" || CommitFull != "abc123full" {
		t.Fatalf("build info = %s %s %s", Version, Commit, CommitFull)
	}

	exitErr := exitCodeError(7)
	if exitErr.ExitCode() != 7 || exitErr.Error() != "exit code 7" {
		t.Fatalf("exitCodeError = %q/%d", exitErr.Error(), exitErr.ExitCode())
	}

	base := errors.New("boom")
	coded := withExitCode(4, base)
	if coded == nil {
		t.Fatal("expected coded error")
	}
	var ce codedError
	if !errors.As(coded, &ce) || ce.ExitCode() != 4 || !errors.Is(coded, base) || coded.Error() != "boom" {
		t.Fatalf("coded error = %#v", coded)
	}
	if withExitCode(4, nil) != nil {
		t.Fatal("nil error should stay nil")
	}
}
