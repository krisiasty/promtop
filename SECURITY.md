# Security policy

## Supported versions

Security fixes go into the latest release. Upgrade to it before reporting.

## Reporting a vulnerability

Report vulnerabilities privately through GitHub's
[private vulnerability reporting](https://github.com/krisiasty/promtop/security/advisories/new).
Do not open a public issue.

Include the promtop version (`promtop --version`), your operating system and
terminal, and the steps or the endpoint payload that trigger the problem.

promtop renders text scraped from untrusted endpoints, so issues such as
terminal escape sequences reaching the screen are in scope.
