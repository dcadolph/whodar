# Getting started with whodar

This guide takes you from nothing to a working setup: build the tool, index a
source, and ask "who do I talk to about X" from the terminal or a browser.

## What whodar does

You give whodar your work data (an org chart, a Slack workspace) and it builds a
local, searchable map of who knows what and which channel to ask in. You then
ask a plain-language question and get a ranked list of people and channels, with
a short reason for each. It works without any model. If you have a local model
through Ollama, it can also write a one-line recommendation.

Everything stays on your machine by default. Nothing is uploaded unless you
explicitly change the policy.

## Requirements

- Go 1.26 or newer to build from source.
- Optional: a Slack bot token if you want to index Slack.
- Optional: Ollama if you want the LLM answer mode.

## Install

Install with Homebrew:

    brew tap dcadolph/whodar
    brew install whodar

Or build from source, which needs Go 1.26 or newer:

    git clone https://github.com/dcadolph/whodar.git
    cd whodar
    go build -o whodar .
    mkdir -p ~/bin && mv whodar ~/bin/

Check it runs:

    whodar version

The examples below use `whodar`. From a source checkout without installing, use
`go run .` from the repository instead.

## Try it in sixty seconds

The repository ships a small example org chart. Index it, then ask a question:

    whodar index --source org-csv --file examples/people.csv
    whodar ask --pretty "who do I talk to about billing retries"

You should see a ranked list of people, each with a score and the reason it
matched, such as "retries (topic)" or "billing (team)". That is the whole loop:
index a source, then ask.

## Index your own org chart

whodar reads a CSV with a header row. Column order does not matter and the
header names are matched loosely, so "Job Title" and "role" both map to title.
Recognized columns:

| Column  | Also accepts                         | Meaning                         |
| ------- | ------------------------------------ | ------------------------------- |
| name    | full name, employee, person          | Display name                    |
| email   | mail, email address                  | Used to merge with other sources |
| title   | job title, role                      | Job title                       |
| team    | department, dept, group              | Team name                       |
| org     | organization, division, business unit | Organization name              |
| manager | manager email, reports to            | Manager identifier              |
| topics  | skills, tags, expertise              | Semicolon-separated topics      |

A row needs at least a name or an email. Topics are split on semicolons by
default. A minimal file looks like this:

    name,email,title,team,topics
    Jane Roe,jane@corp.com,Staff Engineer,Billing,retries;idempotency

Index it:

    whodar index --source org-csv --file /path/to/your/people.csv

## Index Slack

Slack is the strongest source, because it shows which channels exist, what they
are about, and who is active on each topic.

### Create a Slack app and token

1. Go to https://api.slack.com/apps and choose Create New App, then From
   scratch. Pick your workspace.
2. Open OAuth and Permissions. Under Bot Token Scopes add: `channels:read`,
   `channels:history`, `users:read`, and `users:read.email`. To index private
   channels as well, also add `groups:read` and `groups:history`.
3. Install the app to the workspace and copy the Bot User OAuth Token. It starts
   with `xoxb-`.
4. Export it. whodar reads the token only from the environment, never a flag:

       export WHODAR_SLACK_TOKEN=xoxb-your-token

### Run the index

    whodar index --source slack

By default this reads public channels and the last 180 days of history, capped
at 5000 messages per channel. Tune it:

    whodar index --source slack --since-days 90 --max-messages 2000

To include private channels the token can read:

    whodar index --source slack --include-private

### What gets stored

Message text is tokenized and kept in the local index only, alongside who posted
about what. The token itself is never written to disk and never logged. Nothing
is sent to any third party.

## Ask questions

    whodar ask "who owns billing retries"

Output is JSON on stdout, so you can pipe it into other tools. Add `--pretty` to
read it yourself. Useful flags:

- `--limit N` caps results per section (default 5).
- `--mode keyword` (default) ranks with no model.
- `--mode llm` adds a local model. See the next section.

The answer has two sections. People are who to talk to. Channels are where to
ask, each with its most active members for that topic.

## LLM mode

LLM mode retrieves candidates with the index, then asks a local model to rank
them and write a short recommendation. The model only ever sees the retrieved
candidates, so it cannot invent a person or a channel.

1. Install Ollama from https://ollama.com and start it.
2. Pull a model:

       ollama pull llama3.1

3. Ask:

       whodar ask --mode llm "who do I talk to about billing retries"
       whodar ask --mode llm --model qwen2.5 "where do I ask about kafka"

Ollama runs on your machine, so LLM mode is allowed under the default strict
policy. Pointing `--ollama-url` at a non-local host counts as leaving the
machine and is refused unless the policy permits it.

## Semantic search (embeddings)

Keyword search matches words. Semantic search matches meaning, so "who handles
failed payments" can find the person tagged with "billing retries" even with no
shared word. It uses a local embedding model through Ollama.

1. Pull an embedding model:

       ollama pull nomic-embed-text

2. Build the index with embeddings:

       whodar index --source org-csv --file examples/people.csv --embed

3. Search by meaning, or let the LLM use semantic retrieval:

       whodar ask --mode semantic "who handles failed payments"
       whodar ask --mode llm "who handles failed payments"

Embeddings are stored in the index. The semantic mode ranks by them directly,
and the llm mode uses them to retrieve candidates before the model ranks and
summarizes. Set the model with --embed-model. The embedder runs locally, so it
is allowed under the strict policy.

## Web UI

Prefer a search box to the terminal:

    whodar serve

Open http://127.0.0.1:8765, type a question, and pick keyword or llm mode. The
server binds to localhost only, so it is not reachable from the network. Stop it
with Ctrl-C; it shuts down cleanly. Change the address with `--addr`.

## Slack bot

Let your team ask whodar from Slack directly. They mention the bot in a channel
or send it a direct message, and the bot replies in place. Adding `--llm` to a
message uses the model for that answer, and `--keyword` forces the fast path.

### Scopes and events

In addition to the read scopes from the Slack section, add the bot scopes
`chat:write`, `app_mentions:read`, `im:history`, and `im:read`. Under Event
Subscriptions, subscribe the bot to `app_mention` and `message.im`.

### Socket Mode (no public URL)

Best for a laptop or an internal host. Enable Socket Mode, create an app-level
token that starts with `xapp-` with the `connections:write` scope, then run:

    export WHODAR_SLACK_TOKEN=xoxb-...
    export WHODAR_SLACK_APP_TOKEN=xapp-...
    whodar bot --transport socket

### Events API (public endpoint)

Best for a hosted deployment. Point the Slack request URL at
https://your-host/slack/events and run:

    export WHODAR_SLACK_TOKEN=xoxb-...
    export WHODAR_SLACK_SIGNING_SECRET=...
    whodar bot --transport events --addr 0.0.0.0:8766

The events transport verifies the Slack request signature and rejects requests
with an old timestamp.

### Default answer mode

By default the bot answers with the keyword resolver. Set `--mode llm` to make
the model the default, in which case Ollama must run on the bot's host. Either
way, a user overrides per message with a trailing `--llm` or `--keyword`.

## GitHub, Jira, and Confluence

Index code, tickets, and wiki pages to learn who works on what.

GitHub needs a token in WHODAR_GITHUB_TOKEN with repository read access:

    export WHODAR_GITHUB_TOKEN=ghp-...
    whodar index --source github --repo your-org/your-repo
    whodar index --source github --github-org your-org --github-emails

It reads contributors, pull request authors, reviewers, assignees, labels and
titles, non-pull-request issues, repository topics, and CODEOWNERS, weighted by
how much each person works on a topic.

Jira needs a site URL and an API token, created at id.atlassian.com:

    export WHODAR_JIRA_URL=https://your-site.atlassian.net
    export WHODAR_JIRA_EMAIL=you@example.com
    export WHODAR_JIRA_TOKEN=...
    whodar index --source jira --jira-project SEC --jira-project OPS

It reads issue assignees and reporters, weighted by components, labels, summary
words, and project. Use --jira-jql for a custom query. When emails are visible,
these people merge with Slack and the org chart by email.

Confluence uses the same Atlassian site and token as Jira, so the Jira
credentials work, or set WHODAR_CONFLUENCE_URL, WHODAR_CONFLUENCE_EMAIL, and
WHODAR_CONFLUENCE_TOKEN:

    whodar index --source confluence --confluence-space ENG --confluence-space OPS

It reads page creators and last editors, weighted by labels, title words, and
space. Use --confluence-cql for a custom query.

## PagerDuty

Index services and on-call schedules to learn who answers for what. Create a
read-only API token in PagerDuty and export it:

    export WHODAR_PAGERDUTY_TOKEN=...
    whodar index --source pagerduty --merge

It reads every service and the people currently on call, giving each on-call
person the topics of the services they answer for.

## Where your data lives

The index is written to `~/.whodar/index.json` by default. Override the location
with `--data-dir`. This file holds the indexed text, so treat it like the source
data. It is never uploaded.

## Organization policy

Data egress is enforced, not advisory. The default policy is strict: nothing
leaves the machine, and only the keyword resolver and a local model are allowed.

An organization can pin behavior with a policy file. Point whodar at it with the
`WHODAR_POLICY_FILE` environment variable, or place it at `/etc/whodar/policy.json`.
See `examples/policy.json`:

    {
      "mode": "strict",
      "locked": true,
      "private_channels": "deny"
    }

When `locked` is true, user flags cannot loosen the policy. With the file above,
`--policy open` is ignored and `--include-private` is refused. This is how a
cautious organization keeps a managed install locked down while an individual
running their own copy stays free to opt in.

## Updating the index

By default a new run replaces the index, so people who left the org or channels
that went away drop out and new ones appear. To combine sources instead, add
`--merge` and the run adds onto the existing index. People merge by email, so one
human stays one entry across the org chart, Slack, GitHub, Jira, Confluence,
PagerDuty, and code ownership.

Start with the org chart, then merge every other source onto it:

    whodar index --source org-csv --file people.csv
    whodar index --source slack --merge
    whodar index --source github --github-org your-org --merge
    whodar index --source jira --jira-project PROJ --merge
    whodar index --source confluence --confluence-space ENG --merge
    whodar index --source pagerduty --merge
    whodar ask "who knows the billing service"

Each run prints what joined and left since the last index, for example
"+3 people, -1 people, +1 channels". Add `--changes-file changes.json` to write
the full diff as JSON for a script or a report.

## Joining one person across sources

People merge by email. When a source only knows a handle or an account id, such
as a GitHub login without a public email or a CODEOWNERS `@handle`, the same
human shows up as two entries. An alias file declares which identifiers belong
to the same person:

    {
      "alice@corp.com": ["github:alice", "codeowners:alice"]
    }

Pass it once with `--aliases` and the index joins the entries, including ones
indexed before the file existed:

    whodar index --source github --github-org your-org --merge --aliases aliases.json

The mapping is saved in the index, so later runs keep joining without the flag.
Joined identifiers appear in answers under `identities`, and a person's email
always wins as the display identifier. See `examples/aliases.json`.

## Recent activity counts more

Activity ages. Someone who owned a topic three years ago is usually the wrong
person to ask today, so dated records decay: a record loses half its weight per
half-life, 180 days by default. Slack messages, GitHub pull requests and
issues, Jira issues, and Confluence pages all carry their activity date. The
org chart, CODEOWNERS, and PagerDuty on-call describe the present and never
decay.

Tune it at index time:

    whodar index --source slack --merge --half-life-days 90

A shorter half-life favors the people active right now; `--half-life-days 0`
turns decay off entirely.

## Troubleshooting

| Message                                             | Cause                              | Fix                                          |
| --------------------------------------------------- | ---------------------------------- | -------------------------------------------- |
| no index: run `whodar index` first                  | No index built yet                 | Run a `whodar index` command                 |
| invalid arguments: set WHODAR_SLACK_TOKEN           | Slack token not exported           | `export WHODAR_SLACK_TOKEN=xoxb-...`         |
| private-channel ingest is disabled by policy        | Policy denies private channels     | Drop `--include-private` or adjust the policy |
| llm host ...: policy: egress denied                 | Non-local Ollama under strict      | Use a local URL or `--policy open`           |
| llm: request to http://localhost:11434 ... refused  | Ollama is not running              | Start Ollama and pull a model                |
| slack ...: api error: invalid_auth                  | Bad token or missing scope         | Recreate the token with the listed scopes    |
| --policy ignored; pinned by org policy              | A locked org policy is in effect   | Expected. Ask your administrator             |

## Command reference

- `whodar index --source org-csv --file FILE` builds the index from a CSV.
- `whodar index --source slack [--include-private] [--since-days N] [--max-messages N]`
  builds the index from Slack.
- `whodar index --source github (--repo owner/name | --github-org ORG)` indexes GitHub.
- `whodar index --source jira (--jira-project KEY | --jira-jql JQL)` indexes Jira.
- `whodar index --source confluence (--confluence-space KEY | --confluence-cql CQL)` indexes Confluence.
- `whodar index --source pagerduty` indexes PagerDuty services and on-call.
- `whodar index ... --merge` adds the source to the existing index instead of replacing it.
- `whodar ask [--mode keyword|semantic|llm] [--limit N] [--pretty] QUESTION`
  answers a question.
- `whodar index ... --embed` adds embeddings for semantic and llm retrieval.
- `whodar serve [--addr HOST:PORT] [--mode keyword|llm]` runs the web UI.
- `whodar bot [--transport socket|events] [--mode keyword|llm] [--addr HOST:PORT]`
  runs the Slack bot.
- `whodar version` prints the version.

Shared flags: `--data-dir` sets the index location, `--policy` sets the egress
mode, `--pretty` indents JSON.
