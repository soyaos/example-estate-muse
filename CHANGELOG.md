# Changelog

All notable changes to this SoyaPack will be documented in this file.

The format is based on [Keep a Changelog v1.1.0](https://keepachangelog.com/en/1.1.0/),
and this SoyaPack adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0-alpha.0] — 2026-05-21

### Added

- Initial SoyaPack v0 Agent scaffold for **EstateMuse** — the DD-010
  flagship reference Agent that turns a one-sentence brief into a 500-row
  Excel of editorial-grade topic ideas and lets the user spin up a WeChat
  图文 or 30-second 短视频 from any row with one click.
- `soyapack.yaml` manifest declaring `kind: Agent`,
  `virtual_model_id: soya:estate-muse`, both `xlsx` (`topics.v1`) and
  `html` (`topics-table.v1`) artifacts, and a `state.scope=agent` block
  so the row-list persists across per-row action invocations.
- 3-stage chat prompt chain under `prompts/`:
  - `collect.md` — fan one sentence to 60 candidate themes across 8
    market dimensions (buy / hold / sell / market / policy / lifestyle /
    risk / compare).
  - `expand.md` — expand each candidate into 6–10 concrete, action-ready
    topic angles ≥ `target_count + 10`.
  - `dedupe.md` — cluster similar candidates, dim+angle-balance, and emit
    the final `topics.v1` XLSXSnapshot JSON.
- Per-row action prompts:
  - `generate_post.md` — 600–900 字 WeChat 图文 with 3 image suggestions
    and an originality self-check.
  - `generate_video.md` — 30-second 抖音 / 视频号 script with locked
    口播 lines + 画面 cues per beat.
- Templates under `templates/`:
  - `topics.xlsx.tmpl` — reference description of the `topics.v1`
    snapshot the dedupe step emits.
  - `topics.html.tmpl` — shareable HTML table with 生成图文 /
    生成短视频 buttons per row, row-scoped JWTs in the URLs.
- Placeholder example pair under `examples/`.
- Sandbox capability allowlist: `api.openai.com:443/https` egress only,
  `/workdir` read + `/workdir/out` write, `determinism_tier: stateful`,
  `budget_seconds_max: 300` (the brief asks for 5 minutes for the
  initial 500-row run).

[Unreleased]: https://github.com/soyaos/example-estate-muse/compare/v0.1.0-alpha.0...HEAD
[0.1.0-alpha.0]: https://github.com/soyaos/example-estate-muse/releases/tag/v0.1.0-alpha.0
