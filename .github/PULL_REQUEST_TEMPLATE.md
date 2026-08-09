# Pull request

## Summary

<!-- What does this change do, and why is it needed? Keep the summary focused. -->

## Related issue

<!-- Link an issue, for example: Closes #123. Use "N/A" for a docs-only or -->
<!-- maintenance change. -->

## Changes

-

## Validation

<!-- List the commands, tests, or manual checks you ran. Include relevant -->
<!-- output or links. -->

- [ ] `make check` (or explain why a narrower check is appropriate)
- [ ] Integration/API checks when the change affects PostgreSQL or HTTP
      behavior
- [ ] Container/Compose checks when the change affects packaging or deployment

## Review checklist

- [ ] The change is scoped to one coherent purpose and follows the existing
      architecture boundaries.
- [ ] Tests cover new behavior and regressions, or this is not practical for
      the documented reason below.
- [ ] Public API, configuration, migration, and documentation changes are
      called out.
- [ ] No credentials, tokens, personal data, generated binaries, or unrelated
      formatting changes are included.
- [ ] I considered backward compatibility, failure modes, and security implications.
- [ ] I have read and agree to follow the [Code of Conduct](../CODE_OF_CONDUCT.md).

## Breaking changes and rollout notes

<!-- Describe compatibility risks, migration/rollback steps, or write "None". -->
