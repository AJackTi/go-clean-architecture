# Downstream template smoke test

`Template smoke` is a CI workflow that treats this repository as a user would
after selecting **Use this template** on GitHub. It runs on pull requests,
pushes to `main`, and manual dispatches.

The workflow archives the checked-in source tree, extracts it into a disposable
directory with no Git history, initializes a new `main` repository, and then:

1. Runs `cmd/bootstrap` with a representative module, slug, owner, author, and
   maintainer address.
2. Verifies that the module, repository metadata, maintainer references, and
   policy contact are rewritten and that the source template identity is gone.
3. Checks the generated diff, runs `go mod tidy -diff`, `go mod verify`,
   `go vet`, race-enabled tests, and a full build.
4. Commits the generated project, confirms the checkout is clean, and performs
   a second dry-run to prove bootstrap is idempotent.

Run the same check locally from the repository root:

```sh
make template-smoke
```

Running `bash scripts/template-smoke.sh` directly is equivalent.

The source snapshot defaults to `HEAD`, matching a GitHub template checkout.
Set `TEMPLATE_SOURCE_REF` to another committed ref when comparing a release
tag or branch. Set `KEEP_TEMPLATE_SMOKE=1` to retain the disposable checkout
for debugging after the command exits. The assertions tolerate a checkout that
has already been bootstrapped with the smoke target's owner, author, or email;
this keeps the checked-in workflow safe when a generated downstream repository
runs it as part of its own CI.
