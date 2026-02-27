# Changelog

## [0.4.0](https://github.com/Qbandev/kaddons/compare/v0.3.1...v0.4.0) (2026-02-27)


### Features

* add runtime EOL resolution and HTML report output ([31fd3f7](https://github.com/Qbandev/kaddons/commit/31fd3f75624265d2215eb5836c02192df2138dde))
* add stored compatibility resolution and extractor ([#11](https://github.com/Qbandev/kaddons/issues/11)) ([bc6c55a](https://github.com/Qbandev/kaddons/commit/bc6c55a292d5726f364a9bad96c3881dce640bd7))
* make LLM optional with local-only fallback ([#20](https://github.com/Qbandev/kaddons/issues/20)) ([9a2ade3](https://github.com/Qbandev/kaddons/commit/9a2ade3e43c1cee87e2094f4faeadd6049a2e02c))
* runtime EOL resolution and HTML report output ([3a23538](https://github.com/Qbandev/kaddons/commit/3a23538478a993ff30bad5514fe024e712c8b450))


### Bug Fixes

* consolidate release pipeline into single workflow ([#24](https://github.com/Qbandev/kaddons/issues/24)) ([b208eb6](https://github.com/Qbandev/kaddons/commit/b208eb69278a8dc8ac06aaa602e1f9da13923a18))
* document release immutability requirement ([#26](https://github.com/Qbandev/kaddons/issues/26)) ([2c71e24](https://github.com/Qbandev/kaddons/commit/2c71e243980fcbc802fb11f11b5462a96de2c6d9))
* let goreleaser own GitHub release creation ([#17](https://github.com/Qbandev/kaddons/issues/17)) ([db38f10](https://github.com/Qbandev/kaddons/commit/db38f1052e052113a2822be615b44b8cdb968ae2))
* make compatibility analysis input and generation deterministic ([74fbed7](https://github.com/Qbandev/kaddons/commit/74fbed7199af71a99b3b74ff3ce2328cecc0e622))
* preserve constant-delay retry multiplier semantics ([ff43f62](https://github.com/Qbandev/kaddons/commit/ff43f627aacd4e225dee36a450481cc9ffbee94e))
* preserve HTML table structure during evidence fetch ([67cf2a3](https://github.com/Qbandev/kaddons/commit/67cf2a3e396865bd969e8bb3450edf941366b73a))
* prioritize strong compatibility evidence in pruning ([9d25936](https://github.com/Qbandev/kaddons/commit/9d25936bb484f0b87392d2dfcc5046b699a7235a))
* remove skip-github-release to restore tag creation ([#22](https://github.com/Qbandev/kaddons/issues/22)) ([3d60981](https://github.com/Qbandev/kaddons/commit/3d60981a975c95698fe0dd495032713c21f5e6c8))
* retry transient Gemini transport failures ([3cf1579](https://github.com/Qbandev/kaddons/commit/3cf1579c7bcd0ef24576a873995b060c74f83337))
* satisfy errcheck for HTTP response body closes ([c1dd18c](https://github.com/Qbandev/kaddons/commit/c1dd18cea8a425e6f1f82d1266dfaa6ff001fbb8))
* satisfy gosec for html report file writes ([08e4467](https://github.com/Qbandev/kaddons/commit/08e4467fce13c36c6e7b6aaa0b2af1d665e54b4a))
* unblock main workflows for release and tap checks ([#13](https://github.com/Qbandev/kaddons/issues/13)) ([49710cd](https://github.com/Qbandev/kaddons/commit/49710cdb6d8e753abf29bc76e1b45b86cfe0ab22))
* use supported Homebrew directory key in GoReleaser ([#15](https://github.com/Qbandev/kaddons/issues/15)) ([854e2fe](https://github.com/Qbandev/kaddons/commit/854e2fe58bb09c3111d3eff5867a5a53e5d9c179))

## [0.3.1](https://github.com/Qbandev/kaddons/compare/v0.3.0...v0.3.1) (2026-02-27)


### Bug Fixes

* consolidate release pipeline into single workflow ([#24](https://github.com/Qbandev/kaddons/issues/24)) ([b208eb6](https://github.com/Qbandev/kaddons/commit/b208eb69278a8dc8ac06aaa602e1f9da13923a18))
* remove skip-github-release to restore tag creation ([#22](https://github.com/Qbandev/kaddons/issues/22)) ([3d60981](https://github.com/Qbandev/kaddons/commit/3d60981a975c95698fe0dd495032713c21f5e6c8))

## [0.3.0](https://github.com/Qbandev/kaddons/compare/v0.2.2...v0.3.0) (2026-02-27)


### Features

* make LLM optional with local-only fallback ([#20](https://github.com/Qbandev/kaddons/issues/20)) ([9a2ade3](https://github.com/Qbandev/kaddons/commit/9a2ade3e43c1cee87e2094f4faeadd6049a2e02c))

## [0.2.2](https://github.com/Qbandev/kaddons/compare/v0.2.1...v0.2.2) (2026-02-27)


### Bug Fixes

* let goreleaser own GitHub release creation ([#17](https://github.com/Qbandev/kaddons/issues/17)) ([db38f10](https://github.com/Qbandev/kaddons/commit/db38f1052e052113a2822be615b44b8cdb968ae2))

## [0.2.1](https://github.com/Qbandev/kaddons/compare/v0.2.0...v0.2.1) (2026-02-27)


### Bug Fixes

* use supported Homebrew directory key in GoReleaser ([#15](https://github.com/Qbandev/kaddons/issues/15)) ([854e2fe](https://github.com/Qbandev/kaddons/commit/854e2fe58bb09c3111d3eff5867a5a53e5d9c179))

## [0.2.0](https://github.com/Qbandev/kaddons/compare/v0.1.0...v0.2.0) (2026-02-27)


### Features

* add runtime EOL resolution and HTML report output ([31fd3f7](https://github.com/Qbandev/kaddons/commit/31fd3f75624265d2215eb5836c02192df2138dde))
* add stored compatibility resolution and extractor ([#11](https://github.com/Qbandev/kaddons/issues/11)) ([bc6c55a](https://github.com/Qbandev/kaddons/commit/bc6c55a292d5726f364a9bad96c3881dce640bd7))
* runtime EOL resolution and HTML report output ([3a23538](https://github.com/Qbandev/kaddons/commit/3a23538478a993ff30bad5514fe024e712c8b450))


### Bug Fixes

* make compatibility analysis input and generation deterministic ([74fbed7](https://github.com/Qbandev/kaddons/commit/74fbed7199af71a99b3b74ff3ce2328cecc0e622))
* preserve constant-delay retry multiplier semantics ([ff43f62](https://github.com/Qbandev/kaddons/commit/ff43f627aacd4e225dee36a450481cc9ffbee94e))
* preserve HTML table structure during evidence fetch ([67cf2a3](https://github.com/Qbandev/kaddons/commit/67cf2a3e396865bd969e8bb3450edf941366b73a))
* prioritize strong compatibility evidence in pruning ([9d25936](https://github.com/Qbandev/kaddons/commit/9d25936bb484f0b87392d2dfcc5046b699a7235a))
* retry transient Gemini transport failures ([3cf1579](https://github.com/Qbandev/kaddons/commit/3cf1579c7bcd0ef24576a873995b060c74f83337))
* satisfy errcheck for HTTP response body closes ([c1dd18c](https://github.com/Qbandev/kaddons/commit/c1dd18cea8a425e6f1f82d1266dfaa6ff001fbb8))
* satisfy gosec for html report file writes ([08e4467](https://github.com/Qbandev/kaddons/commit/08e4467fce13c36c6e7b6aaa0b2af1d665e54b4a))
* unblock main workflows for release and tap checks ([#13](https://github.com/Qbandev/kaddons/issues/13)) ([49710cd](https://github.com/Qbandev/kaddons/commit/49710cdb6d8e753abf29bc76e1b45b86cfe0ab22))
