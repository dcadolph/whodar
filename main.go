// Command whodar locates the right person or channel for a question.
//
// It indexes people, teams, and topics from work sources, then answers
// "who do I talk to about X" in plain language. Two engines back it: a non-LLM
// keyword ranker and an optional local LLM. Indexed data stays on the machine
// unless an explicit egress policy permits otherwise.
package main

import (
	"fmt"
	"os"
)

// version is the build version, overridden via -ldflags at release time.
var version = "dev"

// usage is the placeholder help text shown until commands land.
const usage = `whodar - find who to talk to about X

usage:
  whodar ask "who owns billing retries"   (coming soon)
  whodar index                            (coming soon)
  whodar version

status: scaffolding. core engine in progress.`

// main runs the whodar CLI and maps any error to a non-zero exit code.
func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "whodar:", err)
		os.Exit(1)
	}
}

// run dispatches the command line. It is a stub pending the cmd package.
func run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "version", "-v", "--version":
			fmt.Println("whodar", version)
			return nil
		}
	}
	fmt.Fprintln(os.Stderr, usage)
	return nil
}
