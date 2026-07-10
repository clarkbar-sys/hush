# Contributing

Thanks for contributing! This guide keeps changes smooth and consistent.

## Workflow

1. **Branch** off `main` using a descriptive name: `feat/short-description`,
   `fix/short-description`, or `docs/short-description`.
2. **Commit** in small, logical units. We follow
   [Conventional Commits](https://www.conventionalcommits.org/) — e.g.
   `feat: add rate limiter`, `fix: handle empty input`.
3. **Test & lint** locally before pushing. Follow the house
   [Coding Standards](./STANDARDS.md) — formatting, linting, types, and the
   coverage floor are all gated in CI, so run the tools before you push.
4. **Open a PR** against `main`, fill out the PR template, and link the issue it closes.
5. At least **one approval** and **green CI** are required before merge.

## Reporting bugs & requesting features

Use the issue templates (Bug report / Feature request). Search existing issues first
to avoid duplicates.

## Code review

- Keep PRs focused and reasonably small — easier to review, faster to merge.
- Respond to review comments; resolve threads once addressed.
- Prefer squash-merge to keep `main` history clean (unless the repo says otherwise).

## Code of Conduct

Participation is governed by our [Code of Conduct](./CODE_OF_CONDUCT.md).
