# example-estate-muse — EstateMuse (DD-010 flagship reference)

> *One sentence in. Five minutes later, a 500-row Excel of editorial-grade
> topic ideas comes out — and every row has a button that spins up a
> WeChat 图文 or 30-second 短视频 in under a minute.*

`example-estate-muse` is the canonical SoyaPack v0 Agent reference for the
[DD-010 · EstateMuse](https://github.com/soyaos/specs) flagship user story.
It is the smallest end-to-end example that exercises every Agent-shaped
edge of the SoyaPack spec that DD-008 / DD-009 did not cover — `xlsx`
artifacts, per-row actions, row-scoped JWTs, and `state.scope=agent`
persistence — without any tooling the user wouldn't see in production.

This repo is **a SoyaPack, not a Go program**. It is meant to be:

- Read top-to-bottom in ~5 minutes to learn the v0 Agent shape with
  per-row actions.
- Built into a `*.soyapack.tar.zst` archive by `soyaos agent build .`.
- Deployed into a SoyaOS Solo or kernel instance and invoked via the
  OpenAI-Compat virtual model id `soya:estate-muse`.

## What EstateMuse does

EstateMuse is a content brain for real-estate self-media operators. The
operator types one sentence — `"杭州亚运村二手房 500 条选题"` — and
EstateMuse:

1. **collect** — fans the brief into 60 candidate themes across 8 market
   dimensions (buy / hold / sell / market / policy / lifestyle / risk /
   compare).
2. **expand** — expands each candidate into 6–10 concrete, action-ready
   topic angles, totalling > 540 raw items.
3. **dedupe** — clusters near-duplicates, dimension+angle-balances the
   list, and emits the final `topics.v1` snapshot the
   [`XLSXRenderer`](https://github.com/soyaos/soyaos/tree/main/pkg/artifact)
   consumes directly.

The result lands as **two** artifacts:

- `topics.v1` — an `.xlsx` workbook (one sheet, 500 rows, frozen header,
  dropdown validation on the dim / angle / difficulty / 建议产物 columns).
- `topics-table.v1` — an HTML companion the operator can share with their
  team: each row carries 生成图文 + 生成短视频 buttons whose URLs embed
  a 24h row-scoped JWT (`pkg/auth/rowtoken.go`).

A click on either button fires
`POST /v1/agents/estate-muse/actions/{action_id}` with `{ row_id, payload }`;
the kernel loads the action's prompt and the upstream LLM produces:

- `wechat_post.v1` — a 600–900 字 WeChat post with image suggestions, or
- `video_script.v1` — a 30-second 抖音 / 视频号 script with 画面 cues.

Both action handlers self-check for originality before returning; if the
output bumps into a known mainstream piece, the prompt sets a
`<!-- ORIGINALITY: REVIEW -->` trailer so the caller can re-generate.

## 5-minute quickstart

```bash
# 1. Build the SoyaPack archive.
soyaos agent build .

# 2. Validate the manifest in isolation.
soyaos agent validate ./soyapack.yaml

# 3. Deploy into a local Solo instance.
soyaos agent deploy ./dist/estate-muse-0.1.0-alpha.0.soyapack.tar.zst

# 4. Trigger the 500-row brain via the OpenAI-Compat chat surface.
#    Stream the XLSXSnapshot body; pipe through the `xlsx` renderer
#    locally if you want a real .xlsx file. (alpha CLI surface still
#    landing — see SoyaOS roadmap for `soyaos agent invoke` status.)
curl http://localhost:6473/v1/chat/completions \
  -H "Authorization: Bearer $SOYA_DEV_KEY" \
  -H "Content-Type: application/json" \
  -d '{
        "model": "soya:estate-muse",
        "messages": [
          {"role":"user","content":"杭州亚运村二手房 500 条选题"}
        ]
      }'

# 5. Trigger a per-row action. row_id is a 1-based index into the
#    rendered workbook; the row-scoped JWT lives in the HTML
#    companion's button href.
curl http://localhost:6473/v1/agents/estate-muse/actions/generate_post \
  -H "Authorization: Bearer $ROW_JWT_FOR_ROW_17" \
  -H "Content-Type: application/json" \
  -d '{
        "row_id": "row-17",
        "payload": {
          "title": "亚运村次新房 2024 全年成交价柱状图",
          "dimension": "market",
          "angle": "数据",
          "hook": "亚运村去年到底涨没涨"
        }
      }'
```

The `soyaos` CLI is still under heavy construction; some of the commands
above will error until the corresponding milestones land (see the parent
SoyaOS roadmap for `soyaos agent build` / `deploy` / `invoke` status).

## Repository layout

```
example-estate-muse/
├── soyapack.yaml             # Canonical v0 Agent manifest.
├── README.md                 # You are here.
├── LICENSE                   # MIT.
├── CHANGELOG.md              # Keep a Changelog v1.1.
├── CODE_OF_CONDUCT.md        # Contributor Covenant v2.1.
├── prompts/
│   ├── collect.md            # Stage 1 — 1 句 → 60 候选.
│   ├── expand.md             # Stage 2 — 60 候选 → 540+ 细选题.
│   ├── dedupe.md             # Stage 3 — 去重 + 落 topics.v1.
│   ├── generate_post.md      # per_row action — WeChat 图文.
│   └── generate_video.md     # per_row action — 30s 短视频脚本.
├── templates/
│   ├── topics.xlsx.tmpl      # XLSXSnapshot reference layout.
│   └── topics.html.tmpl      # Shareable HTML companion with row buttons.
└── examples/
    ├── README.md             # How to add new sample → expected pairs.
    ├── sample-input-1.txt    # Placeholder 一句话 brief.
    ├── expected-topics-1.json# Truncated XLSXSnapshot for the brief.
    └── expected-post-1.md    # Truncated WeChat post for row 1.
```

## Manifest highlights

```yaml
spec_version: soyapack.v0
kind: Agent
name: estate-muse
virtual_model_id: soya:estate-muse
artifacts:
  - { kind: xlsx,     schema: topics.v1 }
  - { kind: html,     schema: topics-table.v1 }
  - { kind: markdown, schema: wechat_post.v1 }
  - { kind: markdown, schema: video_script.v1 }
actions:
  - { id: generate_post,  on: per_row, handler: prompts/generate_post.md,  timeout: 60s, artifacts: [wechat_post] }
  - { id: generate_video, on: per_row, handler: prompts/generate_video.md, timeout: 60s, artifacts: [video_script] }
state:
  scope: agent
  store: kv
sandbox:
  isolation: container
  budget_seconds_max: 300
  capabilities:
    network_out:
      - { host: api.openai.com, port: 443, proto: https }
    fs_read:  [/workdir]
    fs_write: [/workdir/out]
    determinism_tier: stateful
```

The manifest is the contract. The validator at
[`pkg/soyapack.Validate`](https://github.com/soyaos/soyaos/tree/main/pkg/soyapack)
is the authoritative referee — anything it rejects, every SoyaOS runtime
will reject.

## Choosing an upstream model

EstateMuse's quality is highly sensitive to the writer-side model. The
manifest deliberately **omits** `prompt.upstream` so the operator picks
one of:

| Upstream | Verdict | When to use |
|----------|---------|-------------|
| **gpt-4o**             | Recommended for 朋友圈/公众号 长文 quality | Default production deploy. |
| gpt-4o-mini            | Halves cost; topic list reads ~85% of gpt-4o | Smoke tests, internal dogfood. |
| deepseek-chat          | Strong Chinese fluency, weaker on data citations | When the operator's data is closed-loop. |
| local llama-3.1-70b    | No external egress; quality variance high | Offline / sensitive deployments. |

Pin a choice by exporting `SOYA_MODEL_NAME=gpt-4o` (and the matching
`SOYA_MODEL_BASE_URL` + `SOYA_API_KEY`) on the SoyaOS host, or by adding
a `prompt.upstream:` block to the manifest.

## Templates: Go `html/template`, not Nunjucks

Despite some early planning docs that mentioned `.njk` extensions, this
repo uses **`html/template` syntax** because that is what
`pkg/artifact.HTMLRenderer` consumes. The `HTMLRenderer` also
auto-injects the `@media print` CSS block mandated by DESIGN §9, so the
templates here deliberately *do not* repeat those rules — adding them
would only cause two copies to fight at print time.

## Per-row JWT security model

The HTML companion template renders two buttons per row, each carrying a
row-scoped JWT in the `?token=` query. Token semantics:

- **Scope** — bound to (`agent_slug`, `action_id`, `row_id`).
- **Lifetime** — 24 hours; the kernel rejects expired tokens 401.
- **Substitution attack** — token issued for `row-42` posting to
  `row-99` is rejected 401 (covered by
  `pkg/openaicompat/actions_test.go::TestActions_RowTokenForDifferentRowRejected`).
- **Owner key prefix** — the token's `owner_key_prefix` claim is the
  first 12 chars of the sk-soya bearer that minted it; audit logs can
  tie row-token traffic back to its origin without unwrapping the
  bearer.

If you need to share the table with someone whose access should be
revocable separately, mint the JWTs with a short-lived sk-soya bearer
and rotate that bearer when you're done.

## Status

This is **v0.1.0-alpha.0** — the scaffold milestone. The prompts and
templates are deliberately complete enough to render a believable
workbook + post; the sample inputs and expected outputs are placeholders
until the first live editorial operator closes the loop.

| Milestone        | Status |
|------------------|--------|
| Manifest + scaffold (APP-553) | ✓ this release |
| Prompts (chat chain + 2 actions) | ✓ this release |
| HTML / xlsx templates | ✓ this release |
| Real sample + expected output | — pending live editorial run |
| `soyaos agent build` integration | — pending CLI milestone |
| Production deploy via Solo | — pending |
| Originality plugin wiring | — pending APP-5xx |

## License

MIT — see [LICENSE](./LICENSE).
