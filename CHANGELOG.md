# Changelog

## [2.9.1](https://github.com/clarkbar-sys/hush/compare/v2.9.0...v2.9.1) (2026-07-21)


### Bug Fixes

* **web:** stop fleet cards reordering on live status changes ([#202](https://github.com/clarkbar-sys/hush/issues/202)) ([#203](https://github.com/clarkbar-sys/hush/issues/203)) ([0c91c78](https://github.com/clarkbar-sys/hush/commit/0c91c781696d90bbe92faf150371ceb2886e9c22))

## [2.9.0](https://github.com/clarkbar-sys/hush/compare/v2.8.0...v2.9.0) (2026-07-21)


### Features

* **web:** add a Users section under Sessions in the machine view ([#198](https://github.com/clarkbar-sys/hush/issues/198)) ([c92aea9](https://github.com/clarkbar-sys/hush/commit/c92aea97ff437ef0e0ff7d0a389275f2eb765b3d))
* **web:** include the user in the spawned claude session's name prefix ([#199](https://github.com/clarkbar-sys/hush/issues/199)) ([2d5ddc9](https://github.com/clarkbar-sys/hush/commit/2d5ddc94a0adc34aedd0ee2c5ab1c23b3c75bf72))

## [2.8.0](https://github.com/clarkbar-sys/hush/compare/v2.7.0...v2.8.0) (2026-07-21)


### Features

* **sessions:** report installed CLIs and install them system-wide ([#196](https://github.com/clarkbar-sys/hush/issues/196)) ([a2d9432](https://github.com/clarkbar-sys/hush/commit/a2d9432e357c7f0cbef3735d0bdfd4fcab15cfb6))
* **web:** spawn claude sessions with Remote Control enabled ([#193](https://github.com/clarkbar-sys/hush/issues/193)) ([6371c23](https://github.com/clarkbar-sys/hush/commit/6371c23203b7ea1d50d94d2b9a3884eac6a37ec0))

## [2.7.0](https://github.com/clarkbar-sys/hush/compare/v2.6.0...v2.7.0) (2026-07-21)


### Features

* **web:** stop all sessions at once when a machine has 3+ ([#189](https://github.com/clarkbar-sys/hush/issues/189)) ([43bf33d](https://github.com/clarkbar-sys/hush/commit/43bf33d0b1f4f145d57bf5e193fc391c77ae480f))

## [2.6.0](https://github.com/clarkbar-sys/hush/compare/v2.5.0...v2.6.0) (2026-07-20)


### Features

* **sessions:** spawn opencode/claude on any box, tracked read-only ([#145](https://github.com/clarkbar-sys/hush/issues/145)) ([#183](https://github.com/clarkbar-sys/hush/issues/183)) ([d9c6084](https://github.com/clarkbar-sys/hush/commit/d9c6084d9b821a9b7935646ee7d7d08b31313280))
* **web:** add a user on a machine — compose a passwordless-login sudo command ([#185](https://github.com/clarkbar-sys/hush/issues/185)) ([3a064aa](https://github.com/clarkbar-sys/hush/commit/3a064aab933ef34b20849c0f7f84bdb5453edac9))

## [2.5.0](https://github.com/clarkbar-sys/hush/compare/v2.4.1...v2.5.0) (2026-07-20)


### Features

* **web:** export an opencode.json from a machine's exposed LLM ([#181](https://github.com/clarkbar-sys/hush/issues/181)) ([a70ddb3](https://github.com/clarkbar-sys/hush/commit/a70ddb342d036596ac5d991efced7730f9383187))

## [2.4.1](https://github.com/clarkbar-sys/hush/compare/v2.4.0...v2.4.1) (2026-07-20)


### Bug Fixes

* **agent:** report disk usage from /home on immutable-root distros ([#179](https://github.com/clarkbar-sys/hush/issues/179)) ([213116b](https://github.com/clarkbar-sys/hush/commit/213116bf82635d45e114b039a32f65a24c050606))

## [2.4.0](https://github.com/clarkbar-sys/hush/compare/v2.3.1...v2.4.0) (2026-07-20)


### Features

* **backup:** show live restic progress for a run in flight ([#176](https://github.com/clarkbar-sys/hush/issues/176)) ([e488a02](https://github.com/clarkbar-sys/hush/commit/e488a0298add46b51d102543ec50ed85d5b0357b))


### Bug Fixes

* **web:** stop cramming fleet cards two-wide on mobile ([#177](https://github.com/clarkbar-sys/hush/issues/177)) ([cdf2277](https://github.com/clarkbar-sys/hush/commit/cdf227705efeacd0cb86c736bedc8f4a3ac9a029))

## [2.3.1](https://github.com/clarkbar-sys/hush/compare/v2.3.0...v2.3.1) (2026-07-20)


### Bug Fixes

* **agent:** discover LLM runtimes at their bind address, not just loopback ([#174](https://github.com/clarkbar-sys/hush/issues/174)) ([9f1e020](https://github.com/clarkbar-sys/hush/commit/9f1e020268558ed90c8f38f904ec45d7a38ffe0d))

## [2.3.0](https://github.com/clarkbar-sys/hush/compare/v2.2.0...v2.3.0) (2026-07-20)


### Features

* **agent:** disclose local LLM runtimes and how far they reach ([#172](https://github.com/clarkbar-sys/hush/issues/172)) ([f9d542a](https://github.com/clarkbar-sys/hush/commit/f9d542a52c61b3de8a795adf4d8e5f3f8541c34e))

## [2.2.0](https://github.com/clarkbar-sys/hush/compare/v2.1.0...v2.2.0) (2026-07-20)


### Features

* **web:** condense fleet cards with progressive disclosure ([#170](https://github.com/clarkbar-sys/hush/issues/170)) ([c25654d](https://github.com/clarkbar-sys/hush/commit/c25654d005bb4f4730c7383bb849fc21917d5f77))

## [2.1.0](https://github.com/clarkbar-sys/hush/compare/v2.0.0...v2.1.0) (2026-07-19)


### Features

* Cache disk-usage sizing, warm it hourly, add a Re-size button ([#163](https://github.com/clarkbar-sys/hush/issues/163)) ([6dd87a4](https://github.com/clarkbar-sys/hush/commit/6dd87a4aebe7ebb9938cb637cf7e9f48ba0a7f89))
* **web:** show the backup destination on its own card ([#166](https://github.com/clarkbar-sys/hush/issues/166)) ([826f882](https://github.com/clarkbar-sys/hush/commit/826f882944280df4bc835ab4d820567667cc3806))

## [2.0.0](https://github.com/clarkbar-sys/hush/compare/v1.29.0...v2.0.0) (2026-07-19)


### ⚠ BREAKING CHANGES

* the agent no longer serves /backups (create/run/snapshots/ restore) and hush-control no longer proxies /api/machines/{host}/backups*; the -backup, -export-keys, and -state-dir agent flags and the HUSH_AGENT_BACKUP env toggle are gone; /vitals no longer carries the `backup` capability field. Set up and restore backups on the box over SSH (see docs/BACKUP-CONVENTION.md).
* **web:** build button adds a machine directly ([#158](https://github.com/clarkbar-sys/hush/issues/158))
* the agent no longer serves /exec or /jobs; hush-control no longer serves /api/machines/{host}/exec, /api/machines/{host}/jobs, /api/tasks, or /api/workflows; and /vitals and /api/fleet no longer carry runAs/runAsGranted. The -exec, -jobs, and -run-as agent flags (and the matching HUSH_AGENT_* env toggles) are gone.

### Features

* drop the in-agent Backup construct for the read-only convention ([#161](https://github.com/clarkbar-sys/hush/issues/161)) ([face789](https://github.com/clarkbar-sys/hush/commit/face789042e717081ad6a896d12547ab4a92516c))
* remove the Tasks, Jobs, and Workflows constructs ([#156](https://github.com/clarkbar-sys/hush/issues/156)) ([1b57b7f](https://github.com/clarkbar-sys/hush/commit/1b57b7fd9f0a200eb2776a831c4878293a5d34d9))
* **site:** lead the landing page with the live console demo ([#157](https://github.com/clarkbar-sys/hush/issues/157)) ([171faf2](https://github.com/clarkbar-sys/hush/commit/171faf2629253f73e16569f910a1149e97ba57c2))
* **web:** build button adds a machine directly ([#158](https://github.com/clarkbar-sys/hush/issues/158)) ([a62ed11](https://github.com/clarkbar-sys/hush/commit/a62ed11a71857b5c040d06aa170a9673bce3e45c))
* **web:** open a backup in a live watch modal ([#159](https://github.com/clarkbar-sys/hush/issues/159)) ([4a6ee67](https://github.com/clarkbar-sys/hush/commit/4a6ee679e60bc58dfaeac5f6a3bd8dbbd3062935))


### Bug Fixes

* **web:** stop the fleet status legend from wrapping mid-word ([#152](https://github.com/clarkbar-sys/hush/issues/152)) ([00e3c90](https://github.com/clarkbar-sys/hush/commit/00e3c90004a276299dcff4b55ec465a1e80a689f))
* **web:** wrap the fleet summary row instead of overflowing ([#151](https://github.com/clarkbar-sys/hush/issues/151)) ([4d2a4cf](https://github.com/clarkbar-sys/hush/commit/4d2a4cf6848566c780314d75a275af8f1334bc72))

## [1.29.0](https://github.com/clarkbar-sys/hush/compare/v1.28.0...v1.29.0) (2026-07-19)


### Features

* **backups:** detect a run in flight from systemd, not just the marker ([#148](https://github.com/clarkbar-sys/hush/issues/148)) ([fa8eb89](https://github.com/clarkbar-sys/hush/commit/fa8eb89d6e33f4a5934aea23a8aef65c343e4bcf))

## [1.28.0](https://github.com/clarkbar-sys/hush/compare/v1.27.0...v1.28.0) (2026-07-19)


### Features

* **backups:** show a run in progress, distinct from unprotected ([#146](https://github.com/clarkbar-sys/hush/issues/146)) ([1bcf291](https://github.com/clarkbar-sys/hush/commit/1bcf291609c770af9264c79bdf8aad71a17526a3))

## [1.27.0](https://github.com/clarkbar-sys/hush/compare/v1.26.1...v1.27.0) (2026-07-19)


### Features

* **backups:** alert center + browse-inside-a-snapshot; pivot docs ([#138](https://github.com/clarkbar-sys/hush/issues/138)) ([b1ac0b5](https://github.com/clarkbar-sys/hush/commit/b1ac0b53b5b2fc1886b415a6a324dae0f20abf33))
* **console:** make the Fleet map backup-first ([#136](https://github.com/clarkbar-sys/hush/issues/136)) ([c02a5cd](https://github.com/clarkbar-sys/hush/commit/c02a5cd1fbc8d750d2e1b60ee95960089b918e05))

## [1.26.1](https://github.com/clarkbar-sys/hush/compare/v1.26.0...v1.26.1) (2026-07-19)


### Bug Fixes

* **control:** dial agents through the tsnet node so the fleet is visible ([#134](https://github.com/clarkbar-sys/hush/issues/134)) ([d019af7](https://github.com/clarkbar-sys/hush/commit/d019af75aeb72118a26debf8367df66cc9b2b450))

## [1.26.0](https://github.com/clarkbar-sys/hush/compare/v1.25.0...v1.26.0) (2026-07-19)


### Features

* **control:** serve only over the tailnet; remove plain-HTTP LAN mode ([#133](https://github.com/clarkbar-sys/hush/issues/133)) ([84f0f66](https://github.com/clarkbar-sys/hush/commit/84f0f667c5b5277849ed6e0c5cc93a6050f5faa3))


### Bug Fixes

* **systemd:** use Environment= for defaults; ${VAR:-default} isn't valid ([#131](https://github.com/clarkbar-sys/hush/issues/131)) ([e83a2fc](https://github.com/clarkbar-sys/hush/commit/e83a2fc4ed7ae74675f9e7772c42b45166301218))

## [1.25.0](https://github.com/clarkbar-sys/hush/compare/v1.24.0...v1.25.0) (2026-07-19)


### Features

* **backups:** filesystem convention for privileged backups hush can read ([#128](https://github.com/clarkbar-sys/hush/issues/128)) ([55660d7](https://github.com/clarkbar-sys/hush/commit/55660d797bae4d77d7b6bad00b4374591e6a8cc5))
* **backups:** record history and paths, show backups as cards ([#130](https://github.com/clarkbar-sys/hush/issues/130)) ([6dec851](https://github.com/clarkbar-sys/hush/commit/6dec851220dc341d6bf0721a2d86b3011e68f3af))
* **backups:** show root-run backups in the console ([#129](https://github.com/clarkbar-sys/hush/issues/129)) ([4c34418](https://github.com/clarkbar-sys/hush/commit/4c3441899a6b867a8c1a319355e1d6742279c13e))


### Bug Fixes

* **install:** distinguish DNS/network failures from a missing release ([#125](https://github.com/clarkbar-sys/hush/issues/125)) ([d8dffee](https://github.com/clarkbar-sys/hush/commit/d8dffee41098c2848734464af1da90c4b3a67c2e))
* **systemd:** default -listen so a missing env file doesn't crash-loop ([#127](https://github.com/clarkbar-sys/hush/issues/127)) ([4470263](https://github.com/clarkbar-sys/hush/commit/4470263a002513b589ea5fb3a1c60e4dc1c870ca))

## [1.24.0](https://github.com/clarkbar-sys/hush/compare/v1.23.1...v1.24.0) (2026-07-19)


### Features

* **backups:** escrow repo keys off-box via hush-agent -export-keys ([#123](https://github.com/clarkbar-sys/hush/issues/123)) ([1aca499](https://github.com/clarkbar-sys/hush/commit/1aca4993ccd000de0e5c676e473a4cc0dc2d320b))

## [1.23.1](https://github.com/clarkbar-sys/hush/compare/v1.23.0...v1.23.1) (2026-07-19)


### Bug Fixes

* **web:** restart the agent after standing up a vault so it's detected ([#120](https://github.com/clarkbar-sys/hush/issues/120)) ([59bfc81](https://github.com/clarkbar-sys/hush/commit/59bfc81d0425c49648d2b47adcf064082e012d59))

## [1.23.0](https://github.com/clarkbar-sys/hush/compare/v1.22.1...v1.23.0) (2026-07-19)


### Features

* **web:** make the backup sheet Time Machine simple ([#118](https://github.com/clarkbar-sys/hush/issues/118)) ([2805be8](https://github.com/clarkbar-sys/hush/commit/2805be829dba6a0ba4a9ae855ed3645d345a406b))

## [1.22.1](https://github.com/clarkbar-sys/hush/compare/v1.22.0...v1.22.1) (2026-07-18)


### Bug Fixes

* **web:** chain backup setup commands with && to avoid silent partial runs ([#116](https://github.com/clarkbar-sys/hush/issues/116)) ([98a6b36](https://github.com/clarkbar-sys/hush/commit/98a6b36ab2e1308be334be0045d24b5a7baec953))

## [1.22.0](https://github.com/clarkbar-sys/hush/compare/v1.21.0...v1.22.0) (2026-07-18)


### Features

* live htop-style CPU/network panel per machine ([#113](https://github.com/clarkbar-sys/hush/issues/113)) ([3d86731](https://github.com/clarkbar-sys/hush/commit/3d8673170e58dd2a6182fd771e6d581cf20a4950))

## [1.21.0](https://github.com/clarkbar-sys/hush/compare/v1.20.1...v1.21.0) (2026-07-18)


### Features

* generate backup setup commands from detected box state ([#112](https://github.com/clarkbar-sys/hush/issues/112)) ([fac5dd7](https://github.com/clarkbar-sys/hush/commit/fac5dd7fdfd1497ceebc2dc2ce464a995332e48b))
* pick backup paths from the disk-usage treemap ([#108](https://github.com/clarkbar-sys/hush/issues/108)) ([bbda572](https://github.com/clarkbar-sys/hush/commit/bbda572e017be986696b32e96e08867506176f77))
* restic-backed Backup construct (on-demand backups) ([#106](https://github.com/clarkbar-sys/hush/issues/106)) ([e0406f8](https://github.com/clarkbar-sys/hush/commit/e0406f854f93b22dc9c308d202aa9b08bfa457a7))
* restore a snapshot, closing the backup lifecycle ([#111](https://github.com/clarkbar-sys/hush/issues/111)) ([5afd93a](https://github.com/clarkbar-sys/hush/commit/5afd93a82e8610e9cc50cb14ab6feebba2619a91))
* schedule backups to run unattended ([#109](https://github.com/clarkbar-sys/hush/issues/109)) ([447aa23](https://github.com/clarkbar-sys/hush/commit/447aa23f9bee2e64c9e1af736355ae13c2f8f5d1))


### Bug Fixes

* make hush-control mode selection exclusive in the installer ([#110](https://github.com/clarkbar-sys/hush/issues/110)) ([f55c3c1](https://github.com/clarkbar-sys/hush/commit/f55c3c19381b2782e4409eafb42e48e69e1b5298))

## [1.20.1](https://github.com/clarkbar-sys/hush/compare/v1.20.0...v1.20.1) (2026-07-18)


### Bug Fixes

* stop /api/fleet blocking every poll on one offline agent ([#104](https://github.com/clarkbar-sys/hush/issues/104)) ([4e1ad01](https://github.com/clarkbar-sys/hush/commit/4e1ad01a04094102705e6b6a5ae915cd82c1460c))

## [1.20.0](https://github.com/clarkbar-sys/hush/compare/v1.19.0...v1.20.0) (2026-07-18)


### Features

* show per-machine network rx/tx as trend lines under load ([#103](https://github.com/clarkbar-sys/hush/issues/103)) ([8ff910c](https://github.com/clarkbar-sys/hush/commit/8ff910ce01aadee47550c2fadb857911111129b2))


### Bug Fixes

* restart already-running service when re-installing ([#101](https://github.com/clarkbar-sys/hush/issues/101)) ([9bb1859](https://github.com/clarkbar-sys/hush/commit/9bb185944f26c304031e7ca649f5bf96dea0b260))

## [1.19.0](https://github.com/clarkbar-sys/hush/compare/v1.18.0...v1.19.0) (2026-07-18)


### Features

* disk-usage treemap for the Store construct ([#99](https://github.com/clarkbar-sys/hush/issues/99)) ([feb97c9](https://github.com/clarkbar-sys/hush/commit/feb97c9983fc18f82acda7264f3cf84a969a5040))

## [1.18.0](https://github.com/clarkbar-sys/hush/compare/v1.17.0...v1.18.0) (2026-07-16)


### Features

* **agent:** auto-update hush-agent via a root oneshot + timer ([#93](https://github.com/clarkbar-sys/hush/issues/93)) ([c381c37](https://github.com/clarkbar-sys/hush/commit/c381c3722285b992a59cd158db6f83a229b2dd6d))
* **runas:** verify advertised run-as users against the real sudoers grant ([#94](https://github.com/clarkbar-sys/hush/issues/94)) ([1ba8083](https://github.com/clarkbar-sys/hush/commit/1ba8083a1763f3f2f3444d0458a5214b8095cc7e))

## [1.17.0](https://github.com/clarkbar-sys/hush/compare/v1.16.0...v1.17.0) (2026-07-15)


### Features

* **agent:** run Tasks as another user via scoped sudo (-run-as) ([#88](https://github.com/clarkbar-sys/hush/issues/88)) ([3ca634c](https://github.com/clarkbar-sys/hush/commit/3ca634c100f9756779457659153d5aff165e9c39))
* manage a box's run-as users from the console + document run-as ([#92](https://github.com/clarkbar-sys/hush/issues/92)) ([f62231d](https://github.com/clarkbar-sys/hush/commit/f62231d086b4b8be0a96cce4fa3985130c9d33c2))
* pick a Task's run-as user from the console ([#90](https://github.com/clarkbar-sys/hush/issues/90)) ([a11d85f](https://github.com/clarkbar-sys/hush/commit/a11d85f763ff1c2cae4b178aef869d9776b297d6))
* run Workflow steps as another user via scoped sudo ([#91](https://github.com/clarkbar-sys/hush/issues/91)) ([5ca8195](https://github.com/clarkbar-sys/hush/commit/5ca81952f1798d7f4d138c18bb9a8e183bb5a13c))

## [1.16.0](https://github.com/clarkbar-sys/hush/compare/v1.15.0...v1.16.0) (2026-07-14)


### Features

* Job construct — schedule cron commands from the console ([#86](https://github.com/clarkbar-sys/hush/issues/86)) ([c3013a3](https://github.com/clarkbar-sys/hush/commit/c3013a30a1c7dcdee66f2c4ef3170846419d879b))

## [1.15.0](https://github.com/clarkbar-sys/hush/compare/v1.14.0...v1.15.0) (2026-07-13)


### Features

* Job construct — cron-scheduled commands on the agent ([#84](https://github.com/clarkbar-sys/hush/issues/84)) ([4d61af5](https://github.com/clarkbar-sys/hush/commit/4d61af536a8a0d93eb6ccd957f7ff79b37791948))

## [1.14.0](https://github.com/clarkbar-sys/hush/compare/v1.13.0...v1.14.0) (2026-07-13)


### Features

* saved Tasks — the reusable atom Workflows are built from ([#80](https://github.com/clarkbar-sys/hush/issues/80)) ([372d87e](https://github.com/clarkbar-sys/hush/commit/372d87efecab041aa177ad4fac009be9c2e34de1))

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
