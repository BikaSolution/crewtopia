# Security Policy

## Reporting a vulnerability

Please **do not** open a public issue for security problems.

Report vulnerabilities privately through GitHub's
[private vulnerability reporting](https://github.com/BikaSolution/crewtopia/security/advisories/new)
("Security" tab → "Report a vulnerability"). Include steps to reproduce and
the impact you believe it has.

You can expect an acknowledgement within a few days. Fixes are made on
`main`; only the latest version is supported.

## Secrets

This repository never contains real credentials. Configuration secrets live
in git-ignored files (`.env`, `deploy/kratos/secrets.yml`) created from the
committed `*.example` templates. If you believe a secret has been committed,
report it as above so it can be rotated.
