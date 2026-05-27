# Changelog

## [0.7.0] - 2026-05-27

### Features

- **plugins/sigil**: add opencode launcher (#224)
- **plugins**: auto-update sigil plugins (#185)
- **plugins/codex**: support tool call guards (#213)
- **plugins/copilot**: support tool call guards (#214)

### Bug Fixes

- **plugins/sigil**: set service.instance.id per agent session (#218)

### Documentation

- **plugins**: lead with sigil launcher, hide manual install (#210)

## [0.6.0] - 2026-05-26

### Features

- **plugins/sigil**: add local capture mode (#186)

### Bug Fixes

- pin transitive dependencies (#208)
- **plugins/claude**: advance offset only after assistant response (#187)

## [0.5.0] - 2026-05-21

### Features

- **plugins/copilot**: add sigil copilot launcher (#181)
- **plugins/claude**: support SIGIL_GUARDS_* env vars in Claude Code plugin (#178)
- **plugins/copilot**: move copilot plugin into sigil single binary (#176)
- **plugins/codex**: add sigil codex command (#177)

## [0.4.0] - 2026-05-20

### Features

- **plugins/claude**: preserve Claude Code offsets on empty batches (#175)
- **plugins**: add interactive login flow for sigil (#172)
- **plugins**: add copilot plugin (#164)

### Bug Fixes

- **sdk/go**: surface async export failures through Flush() (#171)

### Documentation

- **plugins**: switch install instructions to brew and simplify (#174)

## [0.3.0] - 2026-05-20

### Features

- **plugins/sigil**: add `sigil claude` launcher with plugin bootstrap (#167)
- **plugins/sigil**: add pi launcher subcommand (#166)

## [0.2.0] - 2026-05-19

### Features

- **plugins**: consolidate claude-code, codex, cursor plugin helpers into single binary (#163)
