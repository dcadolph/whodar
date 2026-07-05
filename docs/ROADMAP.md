# Roadmap

whodar answers "who do I talk to about X" from the data it has indexed. The next
versions widen what it can see. Because every source is one connector against a
single interface, each item below is additive and changes nothing downstream.

## More sources

GitHub, Jira, Confluence, and PagerDuty are shipped. Still to come:

- Git history: commit authors per path, for repositories without CODEOWNERS.
- Opsgenie: on-call schedules and service owners.

Each maps its data to people, teams, topics, and channels, and joins other
sources by email or an alias file, so one person stays one entry across them.

## Engine

- Binary vector store: embeddings are kept as JSON today, which is heavy for
  large organizations. A compact on-disk format keeps the index small.
- Incremental indexing: update only what changed instead of rebuilding.
- Confidence: expose how sure the ranking is on each answer.

## Experience

- Result deep links: open a channel or a profile directly from an answer.
- Feedback: let users confirm or correct an answer to improve ranking.
