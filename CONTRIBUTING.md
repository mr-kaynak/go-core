# Contributing to go-core

Thank you for considering contributing to go-core. This guide explains how to get involved.

## Reporting Bugs

Open a [bug report](https://github.com/mr-kaynak/go-core/issues/new?template=bug_report.md) using the issue template. Include a minimal reproduction case, your environment details, and what you expected versus what happened.

## Suggesting Features

Open a [feature request](https://github.com/mr-kaynak/go-core/issues/new?template=feature_request.md). Describe the problem you're solving, your proposed solution, and any alternatives you considered.

## Security Vulnerabilities

Do **not** open a public issue. Follow the process in [SECURITY.md](SECURITY.md).

## Development Setup

### Prerequisites

- Go 1.26+
- Docker and Docker Compose
- Make
- [golangci-lint](https://golangci-lint.run/) v2

### Getting Started

```bash
# Fork and clone the repository
git clone https://github.com/<your-username>/go-core.git
cd go-core

# Install development tools
make install-tools

# Create your local environment file (required — the server will not start
# without JWT_SECRET, SECURITY_ENCRYPTION_KEY, and database settings)
cp .env.example .env

# Start infrastructure services
make docker-up

# Run database migrations
make migrate

# Run the API server
make run
```

## Development Workflow

1. **Fork** the repository and create a branch from `main`.
2. **Branch naming**: use `feat/`, `fix/`, `refactor/`, `docs/`, or `test/` prefixes (e.g., `feat/order-module`).
3. **Write code** following the conventions below.
4. **Write tests** for any new or changed behavior.
5. **Run checks** before pushing:
   ```bash
   make fmt
   make lint
   make test
   ```
   CI additionally enforces a minimum 50% total test coverage — PRs dropping below this threshold fail the build.
6. **Push** your branch and open a pull request against `main`.

## Code Standards

- Run `make fmt` to format code with `gofmt`.
- Run `make lint` to check code with `golangci-lint`.
- Run `make test` to execute all tests (includes `-race` flag).
- Follow the module structure: `domain/` -> `repository/` -> `service/` -> `api/`.
- Use UUIDs for all primary keys.
- Return `*errors.ProblemDetail` from services, never raw strings or generic errors.
- Use structured logging via `slog`. Never log sensitive data.
- Validate input at the handler level using tag-based struct validation.

## Commit Messages

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <description>

[optional body]

[optional footer(s)]
```

**Types**: `feat`, `fix`, `perf`, `refactor`, `docs`, `test`, `ci`, `chore`

**Examples**:
```
feat(blog): add cursor-based pagination to ListPublished
fix(auth): prevent token reuse after password change
perf(blog): replace N+1 queries with single recursive CTE
refactor(cache): extract circuit breaker into shared package
docs: update README with Redis key patterns
test(identity): add integration tests for 2FA enrollment
ci: add PostgreSQL 16 service to test workflow
```

Keep the subject line under 72 characters. Use the body to explain *why*, not *what*.

## Pull Request Guidelines

- Give your PR a descriptive title following the commit message format.
- Link related issues using `Closes #123` or `Fixes #123` in the description.
- One concern per PR. Don't mix unrelated changes.
- Add or update tests for any new or changed behavior.
- Ensure CI passes (`make lint`, `make test`).
- Update documentation if your change affects public APIs or configuration.
- If your change is breaking, note it clearly in the PR description.

## Code Review Process

1. A maintainer will review your PR, usually within a few business days.
2. Address review feedback by pushing new commits (do not force-push during review).
3. Once approved, a maintainer will merge your PR.
4. If your PR goes stale without response, it may be closed after 30 days. Feel free to reopen.

## Releasing

Releases are cut by maintainers only.

1. Update `CHANGELOG.md` — move entries from `[Unreleased]` into a new `[vX.Y.Z]` section with today's date, then update the comparison link at the bottom.
2. Commit the changelog update: `git commit -m "chore: release vX.Y.Z"`.
3. Tag the commit: `git tag vX.Y.Z`.
4. Push the tag: `git push origin vX.Y.Z`.

The `release` workflow triggers automatically on tag push and:
- runs the test suite as a gate,
- builds `api`, `grpc`, and `migrate` binaries for `linux/amd64` and `linux/arm64`,
- builds and pushes Docker images to `ghcr.io/mr-kaynak/go-core-{api,grpc,migrate}` tagged with the version and `latest`,
- creates a GitHub Release with auto-generated notes and the binaries attached.

No manual publish step is required.

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).
