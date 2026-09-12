# Security Policy

## Supported Versions

Security fixes are backported to the latest release and to the `main` branch. Older releases are not actively maintained.

| Version | Supported |
|---------|-----------|
| Latest release | Yes |
| `main` branch | Yes |
| Older releases | No |

## Reporting a Vulnerability

**Do not open a public issue for security vulnerabilities.**

Please report vulnerabilities by emailing **me@mrkaynak.com**, or use GitHub's private vulnerability reporting ([Security Advisories](https://github.com/mr-kaynak/go-core/security/advisories/new)). Include:

- A description of the vulnerability.
- Steps to reproduce or a proof of concept.
- The affected version(s).
- Any potential impact assessment.

## Response Timeline

| Stage           | Timeframe     |
|-----------------|---------------|
| Acknowledgment  | 48 hours      |
| Initial assessment | 7 days     |
| Fix development | Depends on severity |
| Public disclosure | After fix is released |

## Disclosure Policy

We follow coordinated disclosure:

1. The reporter submits the vulnerability privately.
2. We acknowledge receipt within 48 hours.
3. We assess severity and develop a fix.
4. We release the fix and publish a security advisory.
5. The reporter is credited (unless they prefer anonymity).

Please allow us reasonable time to address the issue before any public disclosure.

## Scope

This policy applies to the `mr-kaynak/go-core` repository. If you discover a vulnerability in a third-party dependency, please report it to the upstream maintainers as well.

## Recognition

We appreciate the efforts of security researchers. Contributors who report valid vulnerabilities will be acknowledged in the release notes (with their permission).

## Container scan policy and evidence

The Security workflow rebuilds the API image, reports every vulnerability severity to GitHub code scanning, and fails on unsuppressed findings. A green scan is not proof that a previously published image was rebuilt: consumers must deploy an image built from the patched ref. Base images are pinned by digest; runtime packages are upgraded within Alpine's stable repositories during the build to obtain security patches released ahead of refreshed image tags.

`.trivyignore.yaml` contains a single, expiring, binary-path-scoped exception for [GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932). That advisory affects the unmaintained `golang.org/x/crypto/openpgp` packages, not every package in the module. API, gRPC and migration binaries do not import them. `scripts/check-runtime-crypto.sh` runs inside every Docker build for the requested target architecture and fails if OpenPGP becomes reachable through imports. The source `govulncheck` job separately checks vulnerable call paths. The exception must be reassessed before adding an entry point/build tag or at its expiration; it does not suppress the SSH CVEs fixed by the x/crypto update.
