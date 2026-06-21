# whodar

Find who to talk to about X.

`whodar` answers "who do I talk to about billing retries?" by indexing the people,
teams, topics, and channels across your work tools, then ranking who and where to
ask. The name is who + radar: a radar that finds the right person.

## Why

Even after years inside a large org, the hard question is rarely the work itself.
It is "who owns this?" and "which channel do I post in?". whodar turns scattered
signal (Slack activity, org charts, code ownership, wikis) into a single, queryable
map of expertise.

## How it works

whodar pulls from pluggable connectors, normalizes everything into one graph of
people, teams, and topics, and serves queries through swappable resolvers.

- Connectors fetch raw records from each source (Slack, org and HR exports, wiki
  and code ownership). Adding a source means implementing one small interface.
- The model layer normalizes records into people, teams, orgs, topics, and
  documents, with weighted edges for who talks about what and who owns what.
- The index lives on disk and combines full-text search with affinity scoring.
- Resolvers answer a query. The keyword resolver needs no LLM and always works.
  An optional local LLM resolver adds semantic ranking and a written answer.

## Two modes

- Non-LLM (default): keyword and affinity ranking. No external dependencies,
  deterministic, fast.
- LLM (optional): a local model through Ollama for semantic matching and
  synthesis. More capable, still on the machine.

## Data governance

Indexed work data is sensitive. whodar treats data egress as a first-class,
enforced policy rather than a convention.

- Default policy is strict: nothing leaves the machine. Only the keyword resolver
  and a local LLM are permitted.
- Every external call passes through one policy checkpoint. An adapter cannot
  reach the network unless the policy allows it.
- An organization can pin the policy through a locked config that user flags
  cannot override. A managed deployment stays strict; a personal one can opt in.
- Cloud LLMs are supported by design but disabled under strict policy, gated
  behind explicit opt-in with redaction.

## Frontends

The engine is shared. The CLI ships first. A local web UI, a Slack bot, and a
service reuse the same core.

## Status

Early scaffolding. Building the first vertical slice: an org-chart connector, the
keyword resolver, and the `index` and `ask` commands.

## Build

    go build ./...
    go test ./...
