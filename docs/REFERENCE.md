# Reference

Every command, flag, source, and environment variable. For a guided
walkthrough, start with [GETTING_STARTED.md](GETTING_STARTED.md) instead.

## Global flags

Every command accepts these.

| Flag         | Default     | What it does                                        |
| ------------ | ----------- | --------------------------------------------------- |
| `--data-dir` | `~/.whodar` | Directory holding the index and feedback files.&nbsp;&nbsp;&nbsp; |
| `--policy`   | `strict`    | Egress policy: `strict`, `redacted`, or `open`.     |
| `--pretty`   | off         | Indent JSON output.                                 |

## Sources

Each source is one connector against a single interface; all of them merge
into one index with `--merge`. People join across sources by email, or by an
[alias file](#identity-aliases) when a source only knows a handle.

| Source       | Reads                                        | Credentials                  | Dated  |
| ------------ | -------------------------------------------- | ---------------------------- | ------ |
| `org-csv`    | Org chart CSV: names, titles, teams, topics  | none                         | no     |
| `codeowners` | CODEOWNERS paths per owner                   | none                         | no     |
| `git`        | Commit authors per changed path              | none                         | yes    |
| `slack`      | Users, channels, message history             | `WHODAR_SLACK_TOKEN`         | yes    |
| `github`     | Repos, contributors, PRs, issues, CODEOWNERS | `WHODAR_GITHUB_TOKEN`        | yes    |
| `jira`       | Issue assignees and reporters                | `WHODAR_JIRA_*`              | yes    |
| `confluence` | Page creators and editors                    | `WHODAR_CONFLUENCE_*`        | yes    |
| `pagerduty`  | Services and current on-calls                | `WHODAR_PAGERDUTY_TOKEN`     | no     |

Dated sources decay: see [recency](#recency). Undated sources describe the
present and keep full weight. Bot accounts (dependabot and friends) are
skipped in the git and github sources.

## whodar index

Builds or extends the index from one source per run.

    whodar index --source SOURCE [scope flags] [--merge]

| Flag                | Default   | Applies to | What it does                                     |
| ------------------- | --------- | ---------- | ------------------------------------------------ |
| `--source`          | `org-csv` | all        | Which source to ingest.                          |
| `--merge`           | off       | all        | Add to the existing index instead of replacing.  |
| `--aliases`         |           | all        | JSON alias file joining one person across sources. |
| `--half-life-days`  | `180`     | all        | Days for a dated record's weight to halve; `0` disables decay. |
| `--changes-file`    |           | all        | Write the joiner and leaver diff as JSON.        |
| `--embed`           | off       | all        | Generate embeddings via Ollama for semantic search. |
| `--embed-model`     |           | all        | Ollama embed model (default `nomic-embed-text`). |
| `--ollama-url`      | localhost | all        | Ollama base URL for `--embed`.                   |
| `--file`            |           | org-csv, codeowners | Path to the CSV or CODEOWNERS file (or repo root). |
| `--include-private` | off       | slack      | Ingest private channels if policy allows.        |
| `--since-days`      | `180`     | slack      | History window in days.                          |
| `--max-messages`    | `5000`    | slack      | Message cap per channel.                         |
| `--repo`            |           | github     | Repo as `owner/name`, repeatable.                |
| `--github-org`      |           | github     | Index every repository of an org.                |
| `--max-repos`       | `0` = all | github     | Cap repositories taken from `--github-org`.      |
| `--github-emails`   | off       | github     | Resolve user emails to join other sources.       |
| `--jira-project`    |           | jira       | Project key, repeatable.                         |
| `--jira-jql`        |           | jira       | JQL query; overrides `--jira-project`.           |
| `--jira-url`        |           | jira       | Site URL; or `WHODAR_JIRA_URL`.                  |
| `--max-issues`      | `1000`    | jira       | Cap issues read.                                 |
| `--confluence-space`|           | confluence | Space key, repeatable.                           |
| `--confluence-cql`  |           | confluence | CQL query; overrides `--confluence-space`.       |
| `--max-pages`       | `2000`    | confluence | Cap pages read.                                  |
| `--repo-path`       |           | git        | Local repository root, repeatable.               |
| `--git-since-days`  | `365`     | git        | History window in days.                          |
| `--max-commits`     | `2000`    | git        | Commit cap per repository.                       |

## whodar ask

Answers a question from the index.

    whodar ask [flags] QUESTION

| Flag            | Default   | What it does                                        |
| --------------- | --------- | --------------------------------------------------- |
| `--mode`        | `keyword` | Resolver: `keyword`, `semantic`, or `llm`.          |
| `--limit`       | `5`       | Maximum results per section.                        |
| `--provider`    | `ollama`  | LLM provider: `ollama`, `anthropic`, or `openai`.   |
| `--model`       |           | Chat model for llm mode (defaults per provider).    |
| `--embed-model` |           | Ollama embed model for semantic and llm modes.      |
| `--ollama-url`  | localhost | Ollama base URL.                                    |
| `--openai-url`  |           | OpenAI-compatible base URL, e.g. LM Studio or vLLM. |

Modes: `keyword` needs no model and always works. `semantic` matches on
meaning using embeddings built with `index --embed`. `llm` retrieves
candidates, then a model re-ranks them and writes a short recommendation; it
cannot invent people. The default provider is a local Ollama server. The
`anthropic` (Claude) and `openai` providers are cloud models gated by the
egress policy: strict refuses them, `--policy redacted` sends candidates as
anonymized numbered roles with no names or emails and writes the summary
locally, and `--policy open` sends candidates as-is. The `openai` provider
also speaks to any compatible server via `--openai-url`; a local one, such as
LM Studio, needs no policy opt-in. Each result carries a `confidence`
from zero to one: query coverage scaled by evidence strength, where an
explicit topic is proof, a title slightly less, a passing mention half.

## whodar feedback

Records a vote on an answer so future rankings improve.

    whodar feedback QUESTION (--person ID | --channel NAME) (--helpful | --not-helpful)

| Flag            | What it does                             |
| --------------- | ---------------------------------------- |
| `--person`      | Person identifier from the answer.       |
| `--channel`     | Channel name from the answer.            |
| `--helpful`     | The result answered the question.        |
| `--not-helpful` | The result was wrong for the question.   |

Each net vote multiplies the result's score by 1.25 for that question and its
close variants, clamped at three votes either way. Votes live in
`feedback.json` under the data directory and survive re-indexing.

## whodar serve

Runs the local web UI over the same engine.

    whodar serve [--addr HOST:PORT] [--mode keyword|semantic|llm]

| Flag            | Default          | What it does                          |
| --------------- | ---------------- | ------------------------------------- |
| `--addr`        | `127.0.0.1:8765` | Address to listen on.                 |
| `--mode`        | `keyword`        | Default resolver.                     |
| `--model`       |                  | Ollama chat model for llm mode.       |
| `--embed-model` |                  | Ollama embed model.                   |
| `--ollama-url`  | localhost        | Ollama base URL.                      |

Queries are shareable links: `/?q=who+owns+billing` runs on load. Every
result has feedback buttons.

## whodar demo

Explores whodar on a simulated company: all eight sources are built in
process and served in the web UI, with no credentials and nothing fetched
from the network. Sample data only; it is discarded when the demo stops.

    whodar demo

Takes the same flags as `serve`.

## whodar bot

Runs the Slack bot. Mention it or send it a direct message; a trailing
`--llm` or `--keyword` in a message overrides the mode for that answer.

    whodar bot [--transport socket|events]

| Flag            | Default          | What it does                                 |
| --------------- | ---------------- | -------------------------------------------- |
| `--transport`   | `socket`         | `socket` needs no public URL; `events` serves HTTP. |
| `--addr`        | `127.0.0.1:8766` | Address for the events transport.            |
| `--mode`        | `keyword`        | Default answer mode.                         |
| `--limit`       | `5`              | Maximum results per section.                 |
| `--model`       |                  | Ollama chat model for llm mode.              |
| `--embed-model` |                  | Ollama embed model.                          |
| `--ollama-url`  | localhost        | Ollama base URL.                             |

## Environment variables

Credentials are read only from the environment, never from flags, and are
never logged or stored.

| Variable                      | Used by            | What it is                                |
| ----------------------------- | ------------------ | ----------------------------------------- |
| `WHODAR_SLACK_TOKEN`          | slack source, bot  | Bot token (`xoxb-`).                      |
| `WHODAR_SLACK_APP_TOKEN`      | bot (socket)       | App-level token (`xapp-`).                |
| `WHODAR_SLACK_SIGNING_SECRET` | bot (events)       | Request signature secret.                 |
| `WHODAR_GITHUB_TOKEN`         | github source      | Personal access token.                    |
| `WHODAR_JIRA_URL`             | jira source        | Site URL, e.g. `https://x.atlassian.net`. |
| `WHODAR_JIRA_EMAIL`           | jira source        | Account email for basic auth.             |
| `WHODAR_JIRA_TOKEN`           | jira source        | API token.                                |
| `WHODAR_CONFLUENCE_URL`       | confluence source  | Site URL; falls back to `WHODAR_JIRA_URL`. |
| `WHODAR_CONFLUENCE_EMAIL`     | confluence source  | Account email; falls back to Jira's.      |
| `WHODAR_CONFLUENCE_TOKEN`     | confluence source  | API token; falls back to Jira's.          |
| `WHODAR_PAGERDUTY_TOKEN`      | pagerduty source   | Read-only API token.                      |
| `WHODAR_ANTHROPIC_KEY`        | llm anthropic provider | Claude API key.                       |
| `WHODAR_OPENAI_KEY`           | llm openai provider    | OpenAI-compatible API key.            |
| `WHODAR_POLICY_FILE`          | all commands       | Org policy file path (default `/etc/whodar/policy.json`). |

## Identity aliases

An alias file joins identifiers that belong to the same person when no email
can. The file maps a canonical identifier to its aliases:

    {"alice@corp.com": ["github:alice", "codeowners:alice"]}

Pass it once with `index --aliases`; the mapping persists in the index and
joins entries indexed before the file existed. Joined identifiers appear in
answers under `identities`. See `examples/aliases.json`.

## Recency

Dated records lose half their weight per half-life, 180 days by default, so
today's owner outranks one from years ago. Tune with `--half-life-days` at
index time; `0` disables decay. Undated sources (org chart, CODEOWNERS,
on-call) describe the present and never decay.

## Policy

The egress policy decides what may leave the machine. `strict` (default)
permits nothing external beyond a local model server; non-local `--ollama-url`
and `--openai-url` values and the cloud providers count as egress and are
refused. `redacted` permits cloud providers but strips personal identifiers:
candidates leave as numbered roles, the model returns numbers, and the summary
is written locally. `open` sends candidates as-is. An organization can pin the policy with a
locked file at `WHODAR_POLICY_FILE` or `/etc/whodar/policy.json` that user
flags cannot override, and can deny private-channel ingest the same way. See
`examples/policy.json`.

## Files

Everything lives under `--data-dir` (default `~/.whodar`):

| File            | What it holds                                            |
| --------------- | -------------------------------------------------------- |
| `index.json`    | The graph, postings, embeddings, and identity aliases.   |
| `feedback.json` | User votes, kept apart so they survive re-indexing.&nbsp;&nbsp; |
