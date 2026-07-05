<p align="center">
  <img src="docs/whodar-banner.png" alt="whodar - know who knows" width="100%">
</p>

# whodar

whodar helps you find who to ask at work. Point it at the tools your org already
uses (Slack, GitHub, Jira, Confluence, PagerDuty, git history, an org chart,
code ownership)
and ask a question in plain words. It returns the people to talk to and the
channels to ask in, with the reason it picked each one. It runs locally by
default, with or without an LLM.

## Why

In a large org the hard part is often not the work but knowing who to ask. That
knowledge is spread across Slack, org charts, code ownership, and wikis. whodar
gathers it into one index you can query.

New here? See [docs/GETTING_STARTED.md](docs/GETTING_STARTED.md) for an
end-to-end walkthrough: install, index a source, and ask.

## How it works

whodar pulls from pluggable connectors, normalizes everything into one graph of
people, teams, and topics, and serves queries through swappable resolvers.

- Connectors fetch raw records from each source (Slack, org and HR exports, wiki
  and code ownership). Adding a source means implementing one small interface.
- The model layer normalizes records into people, teams, orgs, topics, and
  documents, with weighted edges for who talks about what and who owns what.
- Identity resolution keeps one human one node. Sources join by email, and an
  alias file joins handle-only identifiers like a GitHub login or a Jira
  account id to the same person.
- The index lives on disk and combines full-text search with affinity scoring.
  Recent activity counts more: dated records lose half their weight per
  half-life (180 days by default), so today's owner outranks one from years ago.
- Resolvers answer a query. The keyword resolver needs no LLM and always works.
  An optional local LLM resolver adds semantic ranking and a written answer.
- Every answer says how sure it is. A confidence score separates a strong
  match from a least-bad one, and each result explains which words hit where.

## Two modes

- Non-LLM (default): keyword and affinity ranking. No external dependencies,
  deterministic, fast.
- LLM (optional): a local model through Ollama for semantic matching and
  synthesis. More capable, still on the machine.

## Data governance

Indexed work data is sensitive, so whodar enforces where it can go rather than
leaving that to convention.

- Default policy is strict: nothing leaves the machine. Only the keyword resolver
  and a local LLM are permitted.
- Every external call passes through one policy checkpoint. An adapter cannot
  reach the network unless the policy allows it.
- An organization can pin the policy through a locked config that user flags
  cannot override. A managed deployment stays strict; a personal one can opt in.
- Cloud LLMs are supported but off under strict policy. Turning one on takes an
  explicit opt-in, with redaction.

## Frontends

All frontends share one engine. The CLI, a local web UI, a Slack bot, and a
service all reuse the same core.

## Status

Working and released. Eight sources (org chart, Slack, GitHub, Jira, Confluence,
PagerDuty, git history, and code ownership), keyword and local LLM answers, a
web UI, and a Slack bot. Prebuilt binaries ship with each release.

## Build

    go build ./...
    go test ./...

## Install

    brew tap dcadolph/whodar
    brew install whodar

Or with Go:

    go install github.com/dcadolph/whodar@latest

Or from source:

    make install        # into $GOBIN
    make build          # ./whodar

Prebuilt binaries are attached to each release. Container images build from the
included Dockerfile. See docs/DEPLOY.md for Docker and systemd deployment,
docs/ARCHITECTURE.md for the design, and CONTRIBUTING.md to add a data source.

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

Licensed under the GNU Affero General Public License v3.0. See [LICENSE](LICENSE).
Copyright 2026 dcadolph.
