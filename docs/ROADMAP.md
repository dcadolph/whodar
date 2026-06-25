# Roadmap

whodar answers "who do I talk to about X" from the data it has indexed. The next
versions widen what it can see. Because every source is one connector against a
single interface, each item below is additive and changes nothing downstream.

## More sources

- GitHub: CODEOWNERS, pull request and issue authors and reviewers, repository
  topics, and team membership, through the GitHub API.
- Jira: issue assignees, reporters, components, and project leads.
- Confluence and other wikis: page authors and space owners.
- PagerDuty or Opsgenie: on-call schedules and service owners.
- Git history: commit authors per path, for repositories without CODEOWNERS.

Each maps its data to people, teams, topics, and channels, and joins other
sources by email, so one person stays one entry across them.

## Engine

- Binary vector store: embeddings are kept as JSON today, which is heavy for
  large organizations. A compact on-disk format keeps the index small.
- Incremental indexing: update only what changed instead of rebuilding.
- Recency and confidence: weight recent activity higher and expose a confidence
  signal on each answer.

## Experience

- Result deep links: open a channel or a profile directly from an answer.
- Feedback: let users confirm or correct an answer to improve ranking.
