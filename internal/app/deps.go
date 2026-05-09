package app

import (
	"context"
	"os"
	"time"

	"github.com/atotto/clipboard"
	"github.com/pbsladek/ai-mr-comment/internal/localgit"
	"github.com/pbsladek/ai-mr-comment/internal/remote"
)

type commandDeps struct {
	now   func() time.Time
	since func(time.Time) time.Duration

	loadConfig func(string) (*Config, error)
	writeFile  func(string, []byte, os.FileMode) error
	clipboard  func(string) error

	isGitRepo          func() bool
	getCurrentBranch   func() (string, error)
	getGitDiff         func(string, bool, []string) (string, error)
	stageAll           func() error
	stageTracked       func() error
	commitMessage      func(string) error
	push               func(string) error
	getRemoteURL       func() (string, error)
	parseRemoteInfo    func(string) (remoteInfo, error)
	getSignoffIdentity func() (string, error)
	editCommitMessage  func(string) (string, error)

	isGitHubHost func(string, string) bool
	isGitLabHost func(string, string) bool
	isGitHubURL  func(string) bool
	isGitLabURL  func(string) bool

	getRemoteMetadata func(context.Context, *Config, string) (prMetadata, error)

	findOrCreateTarget func(context.Context, *Config, remoteInfo, string, string) (string, error)

	getRemoteDiff              func(context.Context, *Config, string) (string, error)
	postRemoteComment          func(context.Context, *Config, string, string) error
	updateRemoteMetadata       func(context.Context, *Config, string, *string, *string) error
	upsertRemoteManagedComment func(context.Context, *Config, string, string) error
	addRemoteLabels            func(context.Context, *Config, string, []string) error
	requestRemoteReviewers     func(context.Context, *Config, string, []string) error
}

func defaultCommandDeps() commandDeps {
	return commandDeps{
		now:   time.Now,
		since: time.Since,

		loadConfig: loadConfigForProfile,
		writeFile:  os.WriteFile,
		clipboard:  clipboard.WriteAll,

		isGitRepo:          localgit.IsRepo,
		getCurrentBranch:   localgit.CurrentBranch,
		getGitDiff:         localgit.Diff,
		stageAll:           localgit.Add,
		stageTracked:       localgit.AddTracked,
		commitMessage:      localgit.CommitMessage,
		push:               localgit.Push,
		getRemoteURL:       localgit.RemoteURL,
		parseRemoteInfo:    remote.ParseInfo,
		getSignoffIdentity: localgit.SignoffIdentity,
		editCommitMessage:  editCommitMessage,

		isGitHubHost: remote.IsGitHubHost,
		isGitLabHost: remote.IsGitLabHost,
		isGitHubURL:  remote.IsGitHubURL,
		isGitLabURL:  remote.IsGitLabURL,

		getRemoteMetadata: getRemoteMetadata,

		findOrCreateTarget: findOrCreateRemoteTarget,

		getRemoteDiff:              getRemoteDiff,
		postRemoteComment:          postRemoteComment,
		updateRemoteMetadata:       updateRemoteMetadata,
		upsertRemoteManagedComment: upsertRemoteManagedComment,
		addRemoteLabels:            addRemoteLabels,
		requestRemoteReviewers:     requestRemoteReviewers,
	}
}

func (d commandDeps) stageQuickCommitChanges(trackedOnly bool) error {
	if trackedOnly {
		return d.stageTracked()
	}
	return d.stageAll()
}
