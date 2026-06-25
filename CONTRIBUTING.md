# Contributing

## Build and test

    make build
    make test
    make vet

Format with gofmt before committing. The project follows the Go standard library
style.

## Adding a data source

A source is one connector. Implement the Source interface in internal/connector,
returning records for people or channels, then add a case to the index command in
cmd/index.go. Nothing else needs to change: the index, resolvers, web UI, and bot
work with the new records automatically. See docs/ARCHITECTURE.md for the layers
and docs/ROADMAP.md for sources planned next.

## Tests

Table-driven tests live alongside the code. Cover the happy path, the error
paths, and edge cases. Run the full suite with make test before opening a change.
