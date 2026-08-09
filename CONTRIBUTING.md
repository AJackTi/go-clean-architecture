# Contributing

Thanks for helping improve this Go Clean Architecture template. Contributions
should make a fresh checkout easier to understand, safer to operate, or more
useful as the starting point for a new service.

## Before you start

1. Search [existing issues](https://github.com/AJackTi/go-clean-architecture/issues)
   and pull requests so that work is not duplicated.
2. For a security vulnerability, follow [SECURITY.md](SECURITY.md) instead of
   opening a public issue.
3. For questions and troubleshooting, read [SUPPORT.md](SUPPORT.md) first.
4. For architectural changes, read [`CONTEXT.md`](CONTEXT.md) and the
   relevant records in [`docs/adr/`](docs/adr/) before proposing a new seam.

## Development setup

Use the Go version declared by `go.mod` (the current toolchain directive is
Go 1.26.5), Docker Compose v2, `make`, and golangci-lint v2.12.2 or newer.

```sh
git clone https://github.com/AJackTi/go-clean-architecture.git
cd go-clean-architecture
cp .env.example .env
make compose-up
curl http://127.0.0.1:8080/api/healthz
```

Run `make help` to see every available target. `make compose-down` stops the
stack while preserving the local PostgreSQL volume; use
`docker compose down --volumes` when you intentionally want a clean database.
Never commit `.env`, credentials, or generated files from `bin/`.

## Quality gates

Before opening a pull request, run the broad local gate:

```sh
make check
```

It covers formatting, module consistency, checksum verification, vet, unit
tests, race-enabled tests, linting, vulnerability scanning, and builds. If a
change affects PostgreSQL, run the integration test against the Compose
database as well:

```sh
database_url='postgres://app:local-dev-password@127.0.0.1:5432/app'
TEST_DATABASE_URL="${database_url}?sslmode=disable" \
  go test -race -count=1 ./internal/item/postgres
```

For HTTP, container, or migration changes, exercise the relevant Compose
workflow and record the commands and results in the pull request. Keep tests
deterministic; avoid relying on wall-clock timing, network services, or shared
state unless the test is explicitly an integration test.

## Project conventions

- Keep business rules in `internal/item`; HTTP and PostgreSQL code belong in
  their adapters. Wire dependencies in `internal/app` rather than reaching
  across layers.
- Propagate `context.Context` to I/O and return errors that callers can map
  consistently. Do not log secrets or user-provided credentials.
- Keep API envelopes, validation rules, pagination semantics, and health/readiness
  behavior backward compatible unless a pull request documents the break.
- Add a focused test for each behavior change. Prefer table-driven unit tests;
  use the race detector for concurrency-sensitive code.
- Database migrations live in `db/migrations`. Once a migration has been
  applied outside a local disposable database, treat it as immutable and add
  a new migration for subsequent changes. Verify upgrade and rollback paths.
- Update README or relevant docs when commands, configuration, API behavior,
  or operational requirements change.

## Branches, commits, and pull requests

- Create a short-lived branch from `main`; do not develop directly on `main`.
- Keep commits small and logically grouped. Use an imperative Conventional
  Commit-style subject where practical (for example, `fix: handle ...` or
  `docs: clarify ...`).
- Open a pull request using the repository template. Explain the motivation,
  user impact, validation performed, and any migration or rollout steps.
- Keep unrelated refactors out of a feature or bug-fix pull request. Resolve
  review feedback with follow-up commits while the review is in progress; a
  maintainer may squash the final history.
- Pull requests must pass the required CI checks and receive review from the
  applicable CODEOWNERS before merge.

## Template customization

If you use this repository as a template, replace the repository URL, owners,
license holder, service name, and deployment defaults for your project. Keep
the security and contribution guidance only if it accurately describes your
fork, and update it whenever your workflow changes.

## License

By contributing, you agree that your contributions are provided under the
[MIT License](LICENSE) that covers this repository.
