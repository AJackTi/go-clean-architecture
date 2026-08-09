# Recommended GitHub repository settings

Git cannot version repository-level settings such as the template flag,
topics, rulesets, or private vulnerability reporting. Apply this checklist to
the canonical repository and repeat the relevant parts after creating a new
service from the template.

## General

- Description: `Production-minded Go Clean Architecture template with PostgreSQL, strict HTTP contracts, Docker Compose, and hardened CI.`
- Website: leave empty unless the project has maintained documentation.
- Mark the repository as a **Template repository**.
- Topics: `go`, `golang`, `clean-architecture`, `hexagonal-architecture`,
  `postgresql`, `gin`, `docker`, `github-template`, `rest-api`, `testing`.
- Keep Issues enabled. Enable Discussions only when maintainers can actively
  support a community channel.
- Allow squash merging, use the pull-request title as the squash commit
  subject, and automatically delete head branches after merge.

## Main branch ruleset

Protect `main` with a repository ruleset:

- require a pull request and at least one approval;
- dismiss stale approvals when the diff changes;
- require review from Code Owners;
- require conversation resolution;
- block force pushes and branch deletion;
- require linear history;
- require these CI jobs to pass:
  - `Go quality`;
  - `PostgreSQL integration`;
  - `Build binaries`;
  - `Go vulnerability scan`;
  - `Docker build and scan`.

Keep the administrator bypass list small. For a one-maintainer fork, allow an
explicit emergency bypass but require a follow-up pull request that documents
why it was used.

## Security

Enable the features available for the repository plan:

- Dependency graph, Dependabot alerts, and Dependabot security updates;
- private vulnerability reporting;
- secret scanning and push protection;
- code scanning/default setup for Go when it adds signal beyond the checked-in
  CI workflow.

Review alerts rather than weakening the checked-in quality gates. A
suppression should name the unreachable path or accepted risk and include a
date for re-evaluation.

## Releases

Create semantic-version tags such as `v1.2.3` (and enforce signed tags when
your team has signing configured). The release workflow
builds `linux`/`darwin` archives for `amd64` and `arm64`, publishes checksums
and an SPDX SBOM, and generates GitHub build-provenance attestations.

Before the first release, confirm that Actions are allowed to create releases
and attestations and that the workflow token has the permissions declared in
`.github/workflows/release.yml`.

## Periodic audit

Quarterly, check that:

- CODEOWNERS contains only active maintainers with write access;
- branch rules reference current CI job names;
- the repository description, topics, social preview, and support links are
  still accurate;
- inactive deploy keys, webhooks, environments, and Actions secrets have been
  removed; and
- dependency/security automation is running rather than merely configured.
