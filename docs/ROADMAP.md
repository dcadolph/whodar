# Roadmap

whodar answers "who do I talk to about X" from the data it has indexed. The next
versions widen what it can see. Because every source is one connector against a
single interface, each item below is additive and changes nothing downstream.

## More sources

GitHub, Jira, Confluence, PagerDuty, and git history are shipped. Still to
come:

- Opsgenie: on-call schedules and service owners.

Each maps its data to people, teams, topics, and channels, and joins other
sources by email or an alias file, so one person stays one entry across them.

## Engine

- Binary vector store: embeddings are kept as JSON today, which is heavy for
  large organizations. A compact on-disk format keeps the index small.
- Incremental indexing: update only what changed instead of rebuilding.

## Experience

- Result deep links: open a channel or a profile directly from an answer.
