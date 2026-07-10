# Coding Standards

House standards for repos created from this template. The goal is *tight and
enforced*: every rule here is backed by a config file and gated in CI, so
"following the standard" mostly means "let the tools run."

| Language   | Format        | Lint / analyze              | Types        | Tests + floor            | Enforced by                     |
| ---------- | ------------- | --------------------------- | ------------ | ------------------------ | ------------------------------- |
| C / C++    | `clang-format`| `clang-tidy`, `cppcheck`, CodeQL | (compiler)   | ASan + UBSan, **≥80%**   | `.github/workflows/ci-c.yml`    |
| Python     | `ruff format` | `ruff`, `bandit`, `pip-audit`    | `mypy --strict` | `pytest`, **≥80%**    | `.github/workflows/ci-python.yml` |
| Scheme (s7)| — parked —    | — parked —                  | —            | —                        | see [§ Scheme](#scheme-s7)      |

Run everything locally before pushing — CI runs the same commands.

---

## C / C++

**Standard:** C sources compile as **C11** (`-std=c11`), C++ sources as
**C++20** (`-std=c++20`). Warnings are errors: `-Wall -Wextra -Werror -pedantic`.

**Formatting** — [`.clang-format`](./.clang-format), **K&R braces**:

```c
int
main(void)
{                     /* function: brace on its own line */
  if (ready) {        /* control statement: brace attached */
    run();
  } else {            /* else on the same line as the close brace */
    wait();
  }
  return 0;
}
```

2-space indent (matches `.editorconfig`), 100-column limit, pointers bind right
(`int *p`). Check locally:

```bash
clang-format --dry-run --Werror $(git ls-files '*.c' '*.h' '*.cpp' '*.cc' '*.hpp')
clang-format -i <file>        # apply
```

**Static analysis** — [`.clang-tidy`](./.clang-tidy) enables the high-signal
families (`bugprone`, `cert`, `clang-analyzer`, `performance`, `portability`,
`readability`). It needs a `compile_commands.json`, so it activates once you
have a cmake build (`-DCMAKE_EXPORT_COMPILE_COMMANDS=ON`); the CI step is stubbed
and commented until then. `cppcheck` and CodeQL run unconditionally.

**Tests** run under **AddressSanitizer and UndefinedBehaviorSanitizer** (matrix),
with `abort_on_error` / `halt_on_error` so a sanitizer finding fails the build.
Both `gcc` and `clang` must build clean.

**Coverage floor: 80%** line coverage, enforced with `gcovr --fail-under-line 80`.

> The build/test steps in `ci-c.yml` are `make` placeholders — wire in your real
> build system (cmake/meson/make) and the flags above will flow through.

---

## Python

**Version:** target **3.11+**; CI tests 3.11 / 3.12 / 3.13. Use a `src/` layout.

**Formatting + linting** — [`pyproject.toml`](./pyproject.toml), one tool:
[`ruff`](https://docs.astral.sh/ruff/) replaces black, isort, flake8, and
pyupgrade. Lint selection is broad (`E W F I N UP B A C4 SIM PTH RUF S`),
100-column lines.

```bash
ruff format .          # apply formatting
ruff format --check .  # CI check
ruff check . --fix     # lint + autofix
```

**Types** — `mypy --strict`. Everything is annotated; no implicit `Any`, no
untyped defs. Config lives in `[tool.mypy]`.

```bash
mypy .
```

**Security** — `bandit` (SAST over `src`) and `pip-audit` (dependency CVEs) both
**gate** — no `|| true`.

**Tests + coverage** — `pytest` with branch coverage; **floor 80%**, enforced via
`--cov-fail-under=80` and `[tool.coverage.report] fail_under`.

```bash
pip install -e '.[dev]'
pytest --cov --cov-report=term-missing
```

---

## Scheme (s7)

**Parked — not yet standardized.** Because s7 ships as source you compile
yourself and its language surface depends on build-time flags, the standard here
will be a **pinned reference-s7 container** (fixed `S7_VERSION` + fixed build
flags) that CI runs against, rather than a formatter. Open decisions: whether to
build with `WITH_PURE_S7`, and which upstream revision to pin. Tracked
separately; `ci-scheme.yml` stays as-is until then.

---

## Cross-cutting

- **Commits:** [Conventional Commits](https://www.conventionalcommits.org/)
  (`feat:`, `fix:`, `chore:` …) — required for release-please. See
  [CONTRIBUTING.md](./CONTRIBUTING.md).
- **Editors:** [`.editorconfig`](./.editorconfig) is the source of truth for
  charset, line endings, final newline, and base indentation. Language
  formatters are configured to agree with it.
- **CI gates before merge:** format check, lint, types, tests at/above the
  coverage floor, and security scans must all be green. Enable branch protection
  on `main` and add the relevant jobs as required checks (see the README
  checklist).
