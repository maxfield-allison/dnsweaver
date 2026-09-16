# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 2.x     | :white_check_mark: |
| < 2.0   | :x:                |

Only the latest patch release of the current major.minor version receives
security updates. Upgrade to the latest release to ensure you have all fixes.

## Reporting a Vulnerability

**Please do not open a public GitHub issue for security vulnerabilities.**

To report a vulnerability, use one of the following methods:

1. **GitHub Security Advisories (preferred):**
   [Open a private security advisory](https://github.com/maxfield-allison/dnsweaver/security/advisories/new)
   on the GitHub repository. This ensures the report remains private until a fix
   is available.

2. **Email:** Send details to the maintainer via the email listed on the
   [GitHub profile](https://github.com/maxfield-allison).

### What to Include

- Description of the vulnerability
- Steps to reproduce (or proof of concept)
- Affected version(s)
- Potential impact assessment
- Suggested fix (if any)

### What to Expect

- **Acknowledgment** within 72 hours of receipt
- **Initial assessment** within 1 week
- **Fix timeline** communicated after assessment — typically within 30 days for
  critical issues, 90 days for lower severity
- **Credit** in the release notes (unless you prefer to remain anonymous)

## Security Practices

dnsweaver follows these security practices:

- **Container image scanning:** GitLab CI uses Trivy to scan the internal amd64 image for CRITICAL and HIGH CVEs. Release images are rebuilt separately, so this check does not by itself establish the contents of a published digest.
- **Dependency scanning:** A pinned `govulncheck` runs in structured mode on GitHub pull requests and relevant GitLab pipelines. Scanner failures, changed reachability and exceptions without a current maintainer decision fail the gate. Accepted exceptions are versioned in `security/govulncheck-exceptions.json` and match the exact advisory, module, version, package set, and reachability class observed during review. Each entry carries its evidence and decision reference, review date, hard expiry, and event-driven review triggers. The public decision and evidence record is `security/govulncheck-exception-review.md`.
- **Secret detection:** Gitleaks scans the checked-out tree in GitLab CI. This is not a claim that every commit in repository history is scanned.
- **Static analysis:** `gosec` runs as part of the linting pipeline
- **Hardened runtime image:** Production images are based on Alpine Linux, run as non-root, and contain no unnecessary packages
- **Minimal attack surface:** No wget, curl, or unnecessary packages in production images
- **Local management surface by default:** Health, readiness, and metrics bind to loopback unless a non-loopback IP and the explicit network-listener opt-in are both configured. The opt-in is not authentication; operators must also restrict network access with a firewall, NetworkPolicy, or equivalent control.
- **Input validation:** Shell metacharacter filtering, HTTP response body limits, and DNS record validation protect their documented boundaries.
- **TLS 1.2 minimum, with TLS 1.3 opt-in:** Every outbound HTTPS connection (provider APIs and the Proxmox source) pins a minimum TLS protocol version of 1.2 by default. Custom CAs (`TLS_CA_FILE`) and mutual-TLS client certificates (`TLS_CERT_FILE` / `TLS_KEY_FILE`) are first-class config; the legacy `INSECURE_SKIP_VERIFY` toggle is retained only as a deprecated alias and emits a startup warning.

## Dependency Management

- Direct dependencies are kept current and reviewed regularly
- Known CVEs in dependencies are tracked in `.trivyignore` with documented justification and review dates
- `govulncheck` findings that cannot yet be fixed on a supported dependency path are tracked separately in `security/govulncheck-exceptions.json`; a changed scope, due review, or expired decision fails the dependency gate
- The project uses `go mod` with `-mod=readonly` to prevent unintended dependency changes

## Disclosure Policy

We follow [coordinated vulnerability disclosure](https://en.wikipedia.org/wiki/Coordinated_vulnerability_disclosure).
We ask that reporters give us reasonable time to address issues before public
disclosure. We will coordinate with you on disclosure timing and credit.
