<p align="center">
  <img src="docs/whodar-banner.png" alt="whodar - know who knows" width="100%">
</p>

# whodar

Know who knows.

`whodar` answers "who do I talk to about billing retries?" by indexing the people,
teams, topics, and channels across your work tools, then ranking who and where to
ask. The name is who + radar: a radar that finds the right person.

## Why

Even after years inside a large org, the hard question is rarely the work itself.
It is "who owns this?" and "which channel do I post in?". whodar turns scattered
signal (Slack activity, org charts, code ownership, wikis) into a single, queryable
map of expertise.

New here? See [docs/GETTING_STARTED.md](docs/GETTING_STARTED.md) for an
end-to-end walkthrough: install, index a source, and ask.

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

## Install

    make install        # into $GOBIN
    make build          # ./whodar

Container images build from the included Dockerfile. See docs/DEPLOY.md for
Docker and systemd deployment, docs/ARCHITECTURE.md for the design, and
CONTRIBUTING.md to add a data source.

## Quickstart

    # Build an index from an org-chart CSV (see examples/people.csv).
    go run . index --source org-csv --file examples/people.csv

    # Or index code ownership (see examples/CODEOWNERS).
    go run . index --source codeowners --file examples/CODEOWNERS

    # Ask who to talk to. Output is JSON; add --pretty to indent.
    go run . ask --pretty "who do I talk to about billing retries"

The index lives under ~/.whodar by default; override with --data-dir. The default
egress policy is strict: nothing leaves the machine.

## Slack

Index a Slack workspace to learn which channels to ask in and who is active on a
topic. Create a Slack app, add the bot scopes `channels:read`, `channels:history`,
`users:read`, and `users:read.email` (add `groups:read` and `groups:history` for
private channels), install it, and export the bot token:

    export WHODAR_SLACK_TOKEN=xoxb-...

    # Standard depth: 180 days, capped at 5000 messages per channel.
    go run . index --source slack

    # Also index private channels the token can read, if policy allows.
    go run . index --source slack --include-private

The token is read only from the environment, never from a flag, and is never
logged or stored. Indexed message text stays on the machine under the strict
policy. Private-channel ingest can be pinned off so user flags cannot enable it.

## LLM mode

By default whodar answers with the keyword resolver, which needs no model. For a
more capable answer, point it at a local Ollama server. whodar retrieves
candidates with the index, then asks the model to rank them and write a short
recommendation. The model only sees retrieved candidates, so it cannot invent
people or channels.

    # Needs a local Ollama (https://ollama.com) with a model pulled.
    ollama pull llama3.1
    go run . ask --mode llm "who do I talk to about billing retries"
    go run . ask --mode llm --model qwen2.5 "where do I ask about kafka"

Ollama runs on the machine, so LLM mode is allowed under the strict policy. A
non-local --ollama-url counts as egress and is refused unless the policy permits
it.

## Semantic search

Build the index with embeddings to match on meaning, not only words:

    ollama pull nomic-embed-text
    go run . index --source org-csv --file examples/people.csv --embed
    go run . ask --mode semantic "who handles failed payments"

The llm mode also retrieves candidates with embeddings when the index has them.
See docs/GETTING_STARTED.md for detail.

## Web UI

Run a local search page over the same engine:

    go run . serve

Open http://127.0.0.1:8765, type a question, and pick keyword or llm mode. The
server binds to localhost only, so nothing leaves the machine. Override the
address with --addr.

## Slack bot

Let people ask whodar from Slack: mention the bot in a channel or send it a
direct message. Add a trailing `--llm` to a message to use the model for that
answer.

The bot needs the read scopes from the Slack section plus `chat:write`,
`app_mentions:read`, `im:history`, and `im:read`, and subscriptions to the
`app_mention` and `message.im` events. Export the bot token:

    export WHODAR_SLACK_TOKEN=xoxb-...

Socket Mode needs no public URL and suits a laptop or internal host. Enable
Socket Mode, create an app-level token (`xapp-`) with `connections:write`, then:

    export WHODAR_SLACK_APP_TOKEN=xapp-...
    whodar bot --transport socket

The Events API suits a hosted deployment with a public HTTPS endpoint. Point the
Slack request URL at https://your-host/slack/events and export the signing
secret:

    export WHODAR_SLACK_SIGNING_SECRET=...
    whodar bot --transport events --addr 0.0.0.0:8766

Set the default answer mode with `--mode keyword|llm`. Socket mode authenticates
with the app token; the events transport verifies the Slack request signature
and rejects stale requests.

## License

Proprietary. All rights reserved. See [LICENSE](LICENSE).
