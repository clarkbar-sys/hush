# Changelog

## [1.13.0](https://github.com/clarkbar-sys/hush/compare/v1.12.0...v1.13.0) (2026-07-13)


### Features

* flag out-of-date agents with a per-machine update popup ([#77](https://github.com/clarkbar-sys/hush/issues/77)) ([dd78eca](https://github.com/clarkbar-sys/hush/commit/dd78ecafbb5d6932b5479972db74448dfe3d68b8))

## [1.12.0](https://github.com/clarkbar-sys/hush/compare/v1.11.0...v1.12.0) (2026-07-13)


### Features

* edit saved workflows in place from the web console ([#73](https://github.com/clarkbar-sys/hush/issues/73)) ([65386d8](https://github.com/clarkbar-sys/hush/commit/65386d80d6d1e926658cc7f52ad98b78a6001fa6))

## [1.11.0](https://github.com/clarkbar-sys/hush/compare/v1.10.0...v1.11.0) (2026-07-13)


### Features

* lint PR titles for Conventional Commits ([#71](https://github.com/clarkbar-sys/hush/issues/71)) ([dc8bd5d](https://github.com/clarkbar-sys/hush/commit/dc8bd5d71c829e38e74951dd4f77a307a702daf0))

## [1.10.0](https://github.com/clarkbar-sys/hush/compare/v1.9.0...v1.10.0) (2026-07-13)


### Features

* add Workflow construct — saved, sequenced multi-step runs ([#64](https://github.com/clarkbar-sys/hush/issues/64)) ([9611ac6](https://github.com/clarkbar-sys/hush/commit/9611ac643f32fdd54c8be80d01ac1a949d32c143))

## [1.9.0](https://github.com/clarkbar-sys/hush/compare/v1.8.0...v1.9.0) (2026-07-13)


### Features

* add Task construct — one-shot commands, streamed live ([#62](https://github.com/clarkbar-sys/hush/issues/62)) ([bd27f99](https://github.com/clarkbar-sys/hush/commit/bd27f99a2dee71ea8de75125708091986449b12d))

## [1.8.0](https://github.com/clarkbar-sys/hush/compare/v1.7.1...v1.8.0) (2026-07-13)


### Features

* **web:** make the console installable as an Android PWA ([#60](https://github.com/clarkbar-sys/hush/issues/60)) ([dcb859b](https://github.com/clarkbar-sys/hush/commit/dcb859b046cb1bb3f79d49df5e9a23a9a69f1f03))

## [1.7.1](https://github.com/clarkbar-sys/hush/compare/v1.7.0...v1.7.1) (2026-07-12)


### Bug Fixes

* **agent:** allow AF_NETLINK so -listen tailnet can resolve its address ([#56](https://github.com/clarkbar-sys/hush/issues/56)) ([fde8783](https://github.com/clarkbar-sys/hush/commit/fde8783f086cd95e049b71e9fb13529a6601cd08))

## [1.7.0](https://github.com/clarkbar-sys/hush/compare/v1.6.0...v1.7.0) (2026-07-12)


### Features

* **control:** add downloadable fleet report ([#52](https://github.com/clarkbar-sys/hush/issues/52)) ([23e7635](https://github.com/clarkbar-sys/hush/commit/23e7635e5a7a180ae15df80be8c6a08cd67d26f6))


### Bug Fixes

* **install:** fall back to a writable bin dir on read-only /usr (SteamOS) ([#49](https://github.com/clarkbar-sys/hush/issues/49)) ([4473de0](https://github.com/clarkbar-sys/hush/commit/4473de054bc2aadb04a82e8470348e181633c1c4))

## [1.6.0](https://github.com/clarkbar-sys/hush/compare/v1.5.0...v1.6.0) (2026-07-12)


### Features

* **web:** show a copy-paste update command in a modal ([43fa3a4](https://github.com/clarkbar-sys/hush/commit/43fa3a4477827a113e66b35971bf94968f609928))


### Bug Fixes

* restart only the installed control unit on self-update ([#43](https://github.com/clarkbar-sys/hush/issues/43)) ([43fa3a4](https://github.com/clarkbar-sys/hush/commit/43fa3a4477827a113e66b35971bf94968f609928))

## [1.5.0](https://github.com/clarkbar-sys/hush/compare/v1.4.0...v1.5.0) (2026-07-12)


### Features

* **agent:** add tailnet listen mode and default installs to it ([#40](https://github.com/clarkbar-sys/hush/issues/40)) ([e126986](https://github.com/clarkbar-sys/hush/commit/e1269866b1ef64ca180fa4ca36d6f3b675772a3b)), closes [#36](https://github.com/clarkbar-sys/hush/issues/36)
* **web:** make a lost hush-control connection loud, not silent demo data ([#35](https://github.com/clarkbar-sys/hush/issues/35)) ([9445f70](https://github.com/clarkbar-sys/hush/commit/9445f70e3ab8525829c1b5e3ec851b99d2787fe4))

## [1.4.0](https://github.com/clarkbar-sys/hush/compare/v1.3.0...v1.4.0) (2026-07-12)


### Features

* background tailnet rescan with a "new agents" badge ([#34](https://github.com/clarkbar-sys/hush/issues/34)) ([cdbeab0](https://github.com/clarkbar-sys/hush/commit/cdbeab001fbf1b3b3ab91652805388bdb2ca3ed2))
* discover tailnet agents from the console (tsnet mode) ([#33](https://github.com/clarkbar-sys/hush/issues/33)) ([7fab268](https://github.com/clarkbar-sys/hush/commit/7fab268699831a66bbf22fea87579ca735461ecf))
* **web:** show each agent's version on the fleet console ([#31](https://github.com/clarkbar-sys/hush/issues/31)) ([540a03c](https://github.com/clarkbar-sys/hush/commit/540a03cf16c97edc8337bbbce9fe47bfc7cd77d7))

## [1.3.0](https://github.com/clarkbar-sys/hush/compare/v1.2.0...v1.3.0) (2026-07-12)


### Features

* add machines to the fleet from the web console ([#25](https://github.com/clarkbar-sys/hush/issues/25)) ([7fe542c](https://github.com/clarkbar-sys/hush/commit/7fe542cb2de95011a4729eba9b448c03295e7e4e))
* version check and self-update for hush-control ([#26](https://github.com/clarkbar-sys/hush/issues/26)) ([bc4edc8](https://github.com/clarkbar-sys/hush/commit/bc4edc80fc4e15a10ce0ded2fddf436e912e3472))

## [1.2.0](https://github.com/clarkbar-sys/hush/compare/v1.1.1...v1.2.0) (2026-07-12)


### Features

* browser-based first-run setup for hush-control tsnet mode ([#21](https://github.com/clarkbar-sys/hush/issues/21)) ([76be60f](https://github.com/clarkbar-sys/hush/commit/76be60f431e16d7500cb0d57ccdaee5b66a52516))
* **web:** lock console to single cyber theme, drop theme toggle ([#24](https://github.com/clarkbar-sys/hush/issues/24)) ([46509d9](https://github.com/clarkbar-sys/hush/commit/46509d936266d10c272d60ab212357bb4f4d38e8))


### Bug Fixes

* allow AF_NETLINK in hush-control-tsnet unit so tsnet can start ([#23](https://github.com/clarkbar-sys/hush/issues/23)) ([ecb49fe](https://github.com/clarkbar-sys/hush/commit/ecb49fe115e3b9cf29bce5ba3aaaa05fd8746d85))

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
