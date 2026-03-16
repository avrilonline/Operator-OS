# Contributing to Operator OS

Thank you for your interest in contributing! This guide covers the development setup, code conventions, and pull request process.

## Getting Started

### Prerequisites

- **Go 1.25+** for backend development
- **Node.js 20+** and **npm** for frontend development
- **Docker** (optional) for containerized testing
- **golangci-lint** for Go linting

### Setup

```bash
git clone https://github.com/operatoronline/Operator-OS.git
cd Operator-OS

# Backend
make deps
make build

# Frontend
cd web
npm install
npm run dev
```

## Development Workflow

1. **Fork** the repository and create a feature branch from `main`
2. **Make your changes** following the conventions below
3. **Test** your changes thoroughly
4. **Commit** using conventional commits
5. **Open a pull request** targeting `main`

## Code Conventions

### Git Commits

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add Telegram webhook retry logic
fix: correct JWT refresh token expiry calculation
docs: add Slack channel setup guide
chore: update Go dependencies
refactor: simplify session eviction policy
test: add integration tests for billing API
```

### Go Backend

- All packages live under `pkg/` in a flat structure (no nested module boundaries)
- Tests are colocated: `foo.go` and `foo_test.go` in the same directory
- `CGO_ENABLED=0` by default for static, portable binaries
- Use `zerolog` for structured logging
- Return errors explicitly; avoid `panic` in library code
- Format with `gofmt` (enforced by CI)

### Frontend (web/)

- **TypeScript strict mode** — no `any` types
- **Mobile-first** — design for 320px first, enhance for larger screens
- **Zustand** for state management — one store per domain
- **Lazy loading** for all route-level pages (`React.lazy()`)
- **OKLCH color tokens** — never use hardcoded color values; use CSS variables
- **No flex-wrap** — use truncation, icon-only states, or horizontal scroll
- **44px minimum touch targets** on mobile interactive elements
- **Phosphor Icons** exclusively for all iconography

### Design System

- 80% monochrome (black, white, grays), 20% functional color
- Single primary hue (260°) for accent; status colors for feedback
- 4px base spacing scale
- DM Sans for text, JetBrains Mono for code
- No colored drop shadows, no gradient backgrounds

## Testing

### Before Submitting a PR

```bash
# Backend
make test          # Go tests
make lint          # Go linter

# Frontend
cd web
npm run typecheck  # TypeScript type checking
npm run lint       # ESLint
npm run build      # Production build (catches bundling issues)
```

All checks must pass before a PR can be merged.

### Writing Tests

- **Go**: Add `_test.go` files next to the code being tested. Use `testify` for assertions.
- **Frontend**: Focus on component logic and store behavior. Test edge cases for accessibility.

## Pull Request Process

1. **Title**: Use a conventional commit-style title (e.g., `feat: add DingTalk channel support`)
2. **Description**: Explain what changed and why. Include screenshots for UI changes.
3. **Size**: Keep PRs focused. Prefer multiple small PRs over one large change.
4. **Reviews**: At least one maintainer approval is required.
5. **CI**: All automated checks must pass (tests, lint, typecheck, build).

## Project Structure

```
cmd/operator/       # CLI entry point
pkg/                # Go packages (flat structure)
web/src/            # React frontend
  components/       # UI components by domain
  pages/            # Route-level pages
  stores/           # Zustand stores
  services/         # API client + WebSocket
  hooks/            # Custom React hooks
  types/            # TypeScript types
config/             # Configuration examples
docker/             # Dockerfiles and compose files
deploy/helm/        # Helm chart
docs/               # Documentation
```

## Reporting Issues

- Use [GitHub Issues](https://github.com/operatoronline/Operator-OS/issues)
- Include steps to reproduce, expected vs actual behavior, and environment details
- For security vulnerabilities, see [SECURITY.md](SECURITY.md)

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).
