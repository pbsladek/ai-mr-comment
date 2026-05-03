# Local Review Examples

## Review the Current Branch Before Opening a PR

Use this while working locally to get a review comment for all changes on the
current branch.

```sh
ai-mr-comment --provider openai
```

Use a more technical prompt when you want implementation-level feedback:

```sh
ai-mr-comment --provider openai --template technical
```

## Review Only Staged Changes

Use this before committing when you want feedback only on what will be included
in the next commit.

```sh
git add api.go main.go
ai-mr-comment --staged --template technical
```

