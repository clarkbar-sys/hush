<!--
  ┌─────────────────────────────────────────────────────────────────────┐
  │  You are reading the template's own README.                          │
  │  After clicking "Use this template", delete this comment block and   │
  │  the "Getting started" section below, then fill in the placeholders. │
  └─────────────────────────────────────────────────────────────────────┘
-->

# {{PROJECT_NAME}}

> {{ONE_LINE_DESCRIPTION}}

<!-- Keep the badge(s) for the language(s) this repo uses; delete the rest. -->
[![CI (C/C++)](https://github.com/clarkbar-sys/{{REPO_NAME}}/actions/workflows/ci-c.yml/badge.svg)](https://github.com/clarkbar-sys/{{REPO_NAME}}/actions/workflows/ci-c.yml)
[![CI (Scheme)](https://github.com/clarkbar-sys/{{REPO_NAME}}/actions/workflows/ci-scheme.yml/badge.svg)](https://github.com/clarkbar-sys/{{REPO_NAME}}/actions/workflows/ci-scheme.yml)
[![CI (Python)](https://github.com/clarkbar-sys/{{REPO_NAME}}/actions/workflows/ci-python.yml/badge.svg)](https://github.com/clarkbar-sys/{{REPO_NAME}}/actions/workflows/ci-python.yml)
[![License](https://img.shields.io/badge/license-GPL--2.0-blue.svg)](./LICENSE)

## Overview

Describe what this project does and who it's for in 2–3 sentences.

## Getting started

```bash
# clone
git clone https://github.com/clarkbar-sys/{{REPO_NAME}}.git
cd {{REPO_NAME}}

# install dependencies
# TODO: add install command for your stack

# run
# TODO: add run command for your stack
```

## Development

```bash
# TODO: lint / test / build commands
```

## Releases

Releases are automated with
[release-please](https://github.com/googleapis/release-please). Commit using
[Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`,
`chore:` …) and a **Release PR** is opened and kept up to date automatically —
it bumps the version and updates [CHANGELOG.md](./CHANGELOG.md). Merge that PR to
tag the release; `release.yml` then builds and attaches the program artifacts.

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md) and the
[Coding Standards](./STANDARDS.md). Please also read our
[Code of Conduct](./CODE_OF_CONDUCT.md).

## Security

Found a vulnerability? See [SECURITY.md](./SECURITY.md) — please do **not** open a
public issue for security reports.

## License

Distributed under the terms of the [GNU GPL v2](./LICENSE).

---

<details>
<summary><strong>📋 New-repo checklist (delete this section once done)</strong></summary>

After creating a repo from this template:

- [ ] Replace `{{PROJECT_NAME}}`, `{{ONE_LINE_DESCRIPTION}}`, `{{REPO_NAME}}` above
- [ ] Keep the CI workflow(s) for your language (`ci-c` / `ci-scheme` / `ci-python`); delete the rest, and delete the badges to match
- [ ] Fill in the `TODO:` build/test commands in the workflow you kept
- [ ] Review [STANDARDS.md](./STANDARDS.md); for Python fill in the
      `{{PLACEHOLDERS}}` in `pyproject.toml`. Configs (`.clang-format`,
      `.clang-tidy`, `pyproject.toml`) enforce an 80% coverage floor by default
- [ ] Update `.github/CODEOWNERS` with the real owning team
- [ ] Uncomment your language's section in `.gitignore` and `dependabot.yml`
- [ ] Enable branch protection on `main` (require CI + 1 review); add the kept
      workflow's job as a required status check
- [ ] Confirm `SECURITY.md` contact is correct
- [ ] Releases: commit with [Conventional Commits](https://www.conventionalcommits.org/)
      so release-please can version + changelog automatically; wire the build step
      in `.github/workflows/release.yml`
- [ ] Delete the "Getting started" TODOs once real commands exist

</details>
