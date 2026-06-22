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

whodar is a private repository, so build it from source:

    git clone git@github.com:dcadolph/whodar.git
    cd whodar
    go build -o whodar .

Move the binary somewhere on your PATH, for example:

    mkdir -p ~/bin && mv whodar ~/bin/

Check it runs:

    whodar version

The examples below use `whodar`. If you did not move the binary, use `go run .`
from the repository instead.

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

Re-run the same index command to refresh. A new run replaces the previous index,
so people who left the org or channels that went away drop out, and new ones
appear. Index Slack and an org chart into the same data directory and whodar
merges people by email, so one human is one entry.

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
- `whodar ask [--mode keyword|llm] [--model NAME] [--limit N] [--pretty] QUESTION`
  answers a question.
- `whodar serve [--addr HOST:PORT] [--mode keyword|llm]` runs the web UI.
- `whodar bot [--transport socket|events] [--mode keyword|llm] [--addr HOST:PORT]`
  runs the Slack bot.
- `whodar version` prints the version.

Shared flags: `--data-dir` sets the index location, `--policy` sets the egress
mode, `--pretty` indents JSON.
