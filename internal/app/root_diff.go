package app

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

func resolveRootDiffInput(cmd *cobra.Command, cfg *Config, opts RootOptions, exclude []string, deps commandDeps) (diffContent, diffSource string, err error) {
	if opts.InputFormat == "json" {
		diffSource = "json"
		var rawInput string
		rawInput, err = readCommandInput(cmd, opts.DiffFilePath)
		if err == nil {
			diffContent, err = decodeAgentInput(rawInput)
		}
		return diffContent, diffSource, err
	}

	if opts.PRURL != "" {
		switch {
		case deps.isGitHubURL(opts.PRURL):
			diffSource = "github-pr: " + opts.PRURL
		case deps.isGitLabURL(opts.PRURL):
			diffSource = "gitlab-mr: " + opts.PRURL
		default:
			err = fmt.Errorf("unsupported URL %q: must be a GitHub PR (/pull/) or GitLab MR (/-/merge_requests/) URL", opts.PRURL)
			return diffContent, diffSource, err
		}
		diffContent, err = deps.getRemoteDiff(cmd.Context(), cfg, opts.PRURL)
		return diffContent, diffSource, err
	}

	if opts.DiffFilePath != "" {
		if opts.DiffFilePath == "-" {
			diffSource = "stdin"
		} else {
			diffSource = "file: " + opts.DiffFilePath
		}
		diffContent, err = readCommandInput(cmd, opts.DiffFilePath)
		return diffContent, diffSource, err
	}

	if commandStdinIsPiped(cmd) {
		diffSource = "stdin"
		diffContent, err = readCommandInput(cmd, "-")
		return diffContent, diffSource, err
	}

	if !deps.isGitRepo() {
		return "", "", errors.New("not a git repository. Run from inside a git repo or use --file to provide a diff")
	}
	switch {
	case opts.Staged:
		diffSource = "git (staged)"
	case opts.Commit != "":
		diffSource = "git (commit: " + opts.Commit + ")"
	default:
		diffSource = "git"
	}
	diffContent, err = deps.getGitDiff(opts.Commit, opts.Staged, exclude)
	return diffContent, diffSource, err
}

func prependLocalBranchContext(diffContent, diffSource string, opts RootOptions, deps commandDeps, cfg *Config) string {
	if opts.PRURL != "" || opts.DiffFilePath != "" || diffSource == "stdin" || diffSource == "json" {
		return diffContent
	}
	if branch, branchErr := deps.getCurrentBranch(); branchErr == nil && branch != "" {
		debugLog(cfg, "branch: name=%s", branch)
		return "Branch: " + branch + "\n\n" + diffContent
	}
	return diffContent
}
