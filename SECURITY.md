# Security Policy

## Supported Versions
Currently, only the latest version of QueryBridge is supported for security updates.

| Version | Supported          |
| ------- | ------------------ |
| Latest  | :white_check_mark: |

## Reporting a Vulnerability
We take security seriously. If you discover a security vulnerability within QueryBridge, please do NOT open a public issue. Instead, please report it privately.

Please send an email to [SECURITY EMAIL] with:
- A description of the vulnerability.
- Steps to reproduce the issue.
- Potential impact if exploited.

We will acknowledge your report within 48 hours and provide a timeline for a fix.

## Security Philosophy
QueryBridge is designed with security in mind:
- **Field Denylist**: Sensitive fields (passwords, tokens) are blocked by default.
- **Read-Only Focus**: The primary goal is querying; write operations should be carefully guarded.
- **Minimal Dependencies**: We strive to keep our dependency tree small and audited.
