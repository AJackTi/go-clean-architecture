# Support

This repository is an open-source template. Support is provided on a best-
effort basis, and downstream services generated from it may have different
configuration, dependencies, or maintainers.

## Before opening a request

1. Read the [README](README.md) and run `make help` to discover the documented
   workflow.
2. Search [existing issues](https://github.com/AJackTi/go-clean-architecture/issues)
   and pull requests for known answers.
3. Reproduce the problem with a clean checkout when possible and record the
   commit or image digest you tested.
4. Remove passwords, tokens, private URLs, and personal data from commands,
   logs, and screenshots.

## Choose the right channel

- **A reproducible defect:** choose the Bug report form from the [new issue
  page](https://github.com/AJackTi/go-clean-architecture/issues/new/choose).
- **A generally useful template improvement:** choose the Feature request form
  from the [new issue page](https://github.com/AJackTi/go-clean-architecture/issues/new/choose).
- **A usage question:** choose the Question or support request form from the
  [new issue page](https://github.com/AJackTi/go-clean-architecture/issues/new/choose).
- **A security vulnerability:** follow [SECURITY.md](SECURITY.md) and report it
  privately. Never publish exploit details in an issue.
- **A pull request:** read [CONTRIBUTING.md](CONTRIBUTING.md), run the relevant
  quality gates, and include validation evidence.

Please do not use a public issue to request private support for a production
deployment. The maintainers cannot access or operate downstream systems.

## What to include

Include the smallest useful reproduction and:

- expected and actual behavior;
- exact commands or HTTP requests;
- commit, tag, or container image digest;
- Go, Docker/Compose, database, OS, and architecture versions; and
- relevant logs with secrets and personal data redacted.

For configuration questions, include the variable names and safe example
values, not the real values. For migration questions, include the migration
version and whether the database is disposable.

## Response expectations

Maintainers review requests when time permits. A response is not guaranteed,
and closing an issue does not imply that a downstream service is fixed. If a
question becomes a repeatable defect or broadly useful improvement, a
maintainer may ask for a minimal reproduction or convert it into a tracked
issue.
