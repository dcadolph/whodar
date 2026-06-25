# Deploying whodar

whodar is a single binary. The web UI and the Slack bot are long-running; the CLI
and indexing are one-shot. Build the index first, then run a frontend against it.

## Docker

Build the image:

    docker build -t whodar .

The image serves the web UI by default. Mount a data directory that holds a
prebuilt index, or run the index command against the same volume first:

    docker run --rm -p 8765:8765 -v whodar-data:/data \
      whodar serve --addr 0.0.0.0:8765 --data-dir /data

For the Slack bot, run the bot subcommand instead and pass the tokens as
environment variables.

## systemd (Slack bot)

Install the binary at /usr/local/bin/whodar and build the index into
/var/lib/whodar. Put the tokens in /etc/whodar/bot.env with mode 0600:

    WHODAR_SLACK_TOKEN=xoxb-...
    WHODAR_SLACK_APP_TOKEN=xapp-...

Install the unit from deploy/whodar-bot.service, then enable it:

    systemctl enable --now whodar-bot

The unit runs as a dedicated user, restarts on failure, and restricts filesystem
access to the data directory.

## Organization policy

To lock a managed deployment to strict egress, place a locked policy at
/etc/whodar/policy.json. See docs/GETTING_STARTED.md for the format.
