# Changelog

## [1.1.1](https://github.com/clarkbar-sys/hush/compare/v1.1.0...v1.1.1) (2026-07-11)


### Bug Fixes

* default install.sh to agent-only, make control opt-in ([#19](https://github.com/clarkbar-sys/hush/issues/19)) ([61a1353](https://github.com/clarkbar-sys/hush/commit/61a13533f3db4ed99b07fbc8537f79edd15a7597))

## [1.1.0](https://github.com/clarkbar-sys/hush/compare/v1.0.0...v1.1.0) (2026-07-11)


### Features

* make install.sh always install as a root-owned systemd service ([#17](https://github.com/clarkbar-sys/hush/issues/17)) ([d098c9b](https://github.com/clarkbar-sys/hush/commit/d098c9b0f4fbacbaad7464f22bec8e4c4b712f97))

## 1.0.0 (2026-07-11)


### Features

* add curl-installable release binaries ([be89f78](https://github.com/clarkbar-sys/hush/commit/be89f78a53df39b1e9feecdbb23adb0be09dc52d))
* **control:** add tsnet mode to serve the console over HTTPS on the tailnet ([5c9266a](https://github.com/clarkbar-sys/hush/commit/5c9266a5ff1fbb78a5f038cf4eccc7bb856eb98c))
* **control:** add tsnet mode to serve the console over HTTPS on the tailnet ([945a9d1](https://github.com/clarkbar-sys/hush/commit/945a9d18691c3e0318c82dc00d7a391b4250e034)), closes [#9](https://github.com/clarkbar-sys/hush/issues/9)
* **control:** embed console UI and document go install ([6cb4ca6](https://github.com/clarkbar-sys/hush/commit/6cb4ca6801a42caad43a64b02918697f58bc3d03))
* **control:** embed console UI and document go install ([2ccd687](https://github.com/clarkbar-sys/hush/commit/2ccd687d667fa055997f904d740f2535c92500c5)), closes [#11](https://github.com/clarkbar-sys/hush/issues/11)
* install hush-agent/hush-control as systemd services ([ea59317](https://github.com/clarkbar-sys/hush/commit/ea5931726532869e830ecb213fc71b9a097b1b0c))
* scaffold Phase 0 fleet console (agent + control plane + UI) ([c67f364](https://github.com/clarkbar-sys/hush/commit/c67f36462f1fcacfaf2f35288a80be20b65da866))
* scaffold Phase 0 fleet console (agent + control plane + UI) ([1edc7fb](https://github.com/clarkbar-sys/hush/commit/1edc7fbaf4f3421672966596d9eb614e7983b9bc)), closes [#6](https://github.com/clarkbar-sys/hush/issues/6)

## Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Releases are automated by [release-please](https://github.com/googleapis/release-please)
from [Conventional Commits](https://www.conventionalcommits.org/) — this file is
updated for you when the Release PR is merged, so you normally don't edit it by hand.

<!-- release-please will insert generated release sections below this line. -->
