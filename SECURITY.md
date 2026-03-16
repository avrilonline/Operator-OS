# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| latest release | Yes |
| previous minor | Security fixes only |
| older | No |

## Reporting a Vulnerability

If you discover a security vulnerability in Operator OS, please report it responsibly.

**Do not open a public GitHub issue for security vulnerabilities.**

### How to Report

1. **Email**: Send a description of the vulnerability to the maintainers via the email listed in the repository's GitHub security advisories tab
2. **GitHub Security Advisories**: Use the [Report a vulnerability](https://github.com/operatoronline/Operator-OS/security/advisories/new) feature on GitHub

### What to Include

- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if you have one)

### Response Timeline

- **Acknowledgment**: Within 48 hours
- **Assessment**: Within 7 days
- **Fix**: Dependent on severity — critical issues are prioritized for immediate patching

## Security Model

### Sandbox

Operator OS agents run in a sandboxed environment by default:

- **Workspace confinement**: File operations are restricted to `~/.operator/workspace`
- **Command filtering**: The `exec` tool blocks dangerous system commands (e.g., `rm -rf /`, disk formatting, system shutdown)
- **Tool iteration limits**: Agents are limited to a configurable number of tool call iterations per turn

### Authentication

- JWT-based authentication with configurable expiry and refresh
- Password hashing with bcrypt
- Rate limiting on auth endpoints
- Optional OAuth integration (Google, GitHub)

### Data Protection

- API keys and secrets are encrypted at rest in the credential store
- GDPR data export and erasure support
- Session data is scoped per user with configurable eviction policies

### Network

- CORS configuration for production domains
- WebSocket connections require authentication
- All external API calls go through configurable proxies
- Channel allowlists (`allow_from`) restrict which users can interact with agents

## Best Practices for Self-Hosting

1. **Keep sandboxing enabled** (`restrict_to_workspace: true`) unless you have a specific reason to disable it
2. **Use strong JWT secrets** — generate with `openssl rand -hex 32`
3. **Set up `allow_from`** on all channels to restrict access to known users
4. **Run behind a reverse proxy** with TLS (nginx, Caddy) in production
5. **Use PostgreSQL** with authentication for multi-tenant deployments
6. **Rotate API keys** regularly and store them in environment variables or a secret manager
7. **Enable command deny patterns** for the `exec` tool in sensitive environments
