# Security policy

Shigoto is early-stage software. Only the latest revision on `main` receives fixes;
there is no supported long-term release branch yet.

## Report a vulnerability

Use [GitHub private vulnerability reporting](https://github.com/Connor-Ryan-1998/shigoto/security/advisories/new)
to report security issues. Include affected versions, impact and reproduction
steps using fictional data. Do not disclose vulnerabilities, credentials or
personal/financial records in public issues.

If private reporting is unavailable, do not post details publicly. Wait for it
to be enabled or use another private channel already established with the
maintainer.

## Self-hosting

Use TLS, keep PostgreSQL private, protect secrets and back up both the database
and uploaded files. Development Compose credentials and Mailpit are not suitable
for production. Review [deployment limitations](docs/development.md#pre-deployment-risks)
before admitting real data; passing tests do not prove live payment, SSO or
disaster-recovery readiness.
