# Pipes and Release Notes Examples

## Pipe a Diff into an Agent or Automation Script

Generate machine-readable review output for another tool:

```sh
git diff main...HEAD | ai-mr-comment --file=- --format json > review.json
```

Inspect only the provider request that would be sent:

```sh
git diff main...HEAD | ai-mr-comment --file=- --print-request > request.json
```

Stream events for an agent runner:

```sh
ai-mr-comment --stream=jsonl --pr "$PR_URL"
```

## Generate Release Notes from a Tag Range

Use the changelog command for release preparation.

```sh
ai-mr-comment changelog \
  --preset release-notes \
  --commit "v1.2.0..HEAD"
```

Write the result to a file:

```sh
ai-mr-comment changelog \
  --preset release-notes \
  --commit "v1.2.0..HEAD" \
  --output RELEASE_NOTES.md
```

