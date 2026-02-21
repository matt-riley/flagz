# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 1.0.x   | :white_check_mark: |
| < 1.0   | :x:                |

Only the latest release receives security updates. If you're running an older version, please upgrade before reporting.

---

## Reporting a Vulnerability

**Do not open a public issue for security vulnerabilities.**

Instead, please use one of the following:

1. **GitHub Security Advisories (preferred):** Open a private advisory via the [Security tab](https://github.com/matt-riley/flagz/security/advisories/new) of this repository.
2. **Email:** Send details to **security@flagz.dev**.

We take all reports seriously and will acknowledge them promptly.

---

## What to Include

A good report helps us fix the issue faster. Please provide:

- **Description** — What is the vulnerability and which component is affected?
- **Steps to reproduce** — Minimal, concrete steps to trigger the issue.
- **Impact assessment** — What can an attacker achieve? (e.g. data exposure, denial of service, authentication bypass)
- **Environment** — Version of flagz, Go version, OS, and any relevant configuration.
- **Proof of concept** — Code, curl commands, or logs demonstrating the issue (if available).

If you're unsure about severity, report it anyway — we'd rather triage a false alarm than miss a real one.

---

## Response Timeline

| Step                      | Target          |
| ------------------------- | --------------- |
| Acknowledge your report   | Within 48 hours |
| Initial assessment        | Within 7 days   |
| Fix for critical issues   | Within 30 days  |
| Fix for non-critical issues | Best effort   |

We'll keep you informed as we work through the fix. If we need more information, we'll reach out using the channel you contacted us on.

---

## Security Considerations

flagz is a self-hosted service. That means **you** own the deployment, the network boundary, and the keys. Here's what to keep in mind:

### Authentication

All `/v1/*` HTTP endpoints and all gRPC methods require a **bearer token** of the form `<api_key_id>.<raw_secret>`. Secrets are stored as salted **bcrypt** hashes in the `api_keys` table. Legacy SHA-256 hashes are still accepted for backward compatibility but bcrypt is strongly recommended for new keys.

Token comparison uses constant-time operations to prevent timing attacks.

### Unauthenticated Endpoints

`GET /healthz` and `GET /metrics` are **intentionally unauthenticated** — they sit outside the `/v1/*` auth gate by design. If exposing health or metrics data is a concern in your environment, restrict access at the network level (firewall rules, reverse proxy, network policy, etc.).

### Network Boundary

flagz does not phone home, does not require internet access, and does not manage TLS termination itself. You are responsible for:

- Placing flagz behind a reverse proxy or load balancer that handles TLS.
- Restricting network access to the HTTP (`:8080`) and gRPC (`:9090`) ports.
- Ensuring the PostgreSQL connection is encrypted or on a trusted network.

### Database Credentials

`DATABASE_URL` contains your PostgreSQL connection string, including credentials. Keep it out of source control, container image layers, and logs. Use secrets management (environment variables injected at runtime, orchestrator secrets, vault, etc.).

### API Key Management

API keys are currently managed directly in the `api_keys` database table. Rotate keys periodically and revoke any that may have been compromised. There is no built-in key expiry — treat key lifecycle as your responsibility.

---

## Disclosure Policy

We follow a **coordinated disclosure** process:

1. You report the vulnerability privately.
2. We acknowledge, assess, and develop a fix.
3. We release the fix and publish a security advisory.
4. You are free to discuss the vulnerability publicly after the fix is released.

We will **credit reporters** in the advisory unless you prefer to remain anonymous. Just let us know your preference when you report.

---

Thank you for helping keep flagz and its users safe.
