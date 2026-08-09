# Security policy

## Supported versions

This repository is a template rather than a long-lived release line. Security
fixes are developed on `main`; use the newest commit or release when possible.

| Version | Supported |
| --- | --- |
| `main` | Yes |
| Older commits or unmaintained forks | No |

Downstream services created from this template are responsible for their own
dependency, deployment, and secret-management policies.

If you copy this repository as a template, replace the repository URLs,
maintainer contact, and supported-version table before publishing your fork.

## Reporting a vulnerability

Please do not disclose a suspected vulnerability in a public issue, pull
request, or discussion. Use GitHub's private vulnerability reporting flow:

<https://github.com/AJackTi/go-clean-architecture/security/advisories/new>

If private reporting is unavailable for your account or fork, contact the
maintainers through the address associated with the repository owner
(`dtrong97vn@gmail.com`) and include **Security report** in the subject. Do not
send credentials, production data, or a weaponised exploit; redact sensitive
values and provide the smallest reproducible proof instead.

Include, when available:

- the affected commit, tag, or image digest;
- the impact and a clear description of the attack or failure mode;
- reproducible steps or a minimal proof of concept;
- affected configuration, dependency, or deployment assumptions; and
- any mitigation you have already applied.

We aim to acknowledge reports within seven days. The maintainers will triage
the report, coordinate a fix or mitigation, and agree on a disclosure date with
the reporter; timelines may vary with severity and maintainer availability.
Please allow time for downstream template users to update their generated
services before public disclosure.

## Scope and safe testing

Only test against local copies or systems for which you have explicit
authorization. Do not access, alter, or exfiltrate another user's data; do not
perform denial-of-service tests; and do not publish private report contents
before a fix and disclosure plan are agreed.
