# example-estate-muse — EstateMuse (DD-010 flagship reference)

> [!WARNING]
> **This project is under active development and has not been formally
> released. Features and APIs are unstable; breaking changes may happen at
> any time. Do not depend on this alpha in production.**
>
> **本项目仍在开发中，尚未正式发布。功能尚不稳定，接口和行为随时可能发生
> breaking change，请勿将当前 alpha 版本作为生产依赖。**

> *One sentence in. Within five minutes, a 500-row Excel of editorial topic
> ideas comes out. Any row can generate a WeChat post or short-video script
> in under one minute.*

EstateMuse is the canonical SoyaPack v0 reference for the
[DD-010 user story](https://github.com/soyaos/specs). It demonstrates the
parts that distinguish a stateful Agent from a one-shot prompt:

- a three-step prompt chain: `collect → expand → dedupe`;
- a real `topics.v1` Excel artifact with 500 rows;
- per-row `generate_post` and `generate_video` actions;
- agent-scoped persistence that survives a SoyaOS restart;
- row-scoped JWT authorization for narrowly shared action links.

This repository is a declarative SoyaPack—YAML, prompts, templates, examples,
and E2E tests—not a standalone Go application.

## What works today

The current SoyaOS development build can execute this complete operator path:

1. validate and build the Pack into a reproducible `.spk`;
2. deploy it to a running Solo process;
3. invoke `soya:estate-muse` and render the returned snapshot as a real `.xlsx`;
4. persist the workbook and each row's original context in bbolt;
5. run a row action using the persisted row as the authoritative payload;
6. restart SoyaOS and run the same row action without regenerating the workbook.

The HTML companion template and originality tool declaration are present, but
their automatic runtime orchestration is not complete. See
[Alpha limitations](#alpha-limitations).

## Prerequisites

- Go 1.23.x to build the sibling SoyaOS checkout;
- a current `soyaos` binary built from
  [soyaos/soyaos](https://github.com/soyaos/soyaos);
- an OpenAI-compatible model endpoint and API key;
- this repository next to the SoyaOS repository when running E2E tests:

```text
workspace/
├── soyaos/
└── example-estate-muse/
```

## Quickstart

### 1. Start SoyaOS

Open terminal A. Configure your OpenAI-compatible provider, then keep the
process running:

```bash
export SOYA_MODEL_API_KEY='replace-with-your-key'
export SOYA_MODEL_BASE_URL='https://api.openai.com/v1'
export SOYA_MODEL_DEFAULT='gpt-4o'

soyaos start \
  --listen 127.0.0.1:7474 \
  --rpc 127.0.0.1:7475 \
  --data-dir "$PWD/.soyaos-data"
```

The local development key is `sk-soya-dev-local`. Treat any non-development
key like a password and never commit it.

### 2. Validate, build, and deploy the Pack

Open terminal B in this repository:

```bash
soyaos pack validate .
soyaos agent build .
soyaos agent deploy \
  ./dist/estate-muse-0.1.0-alpha.0.spk \
  --rpc http://127.0.0.1:7475
```

### 3. Generate a real 500-row Excel workbook

```bash
mkdir -p ./dist/trial

time soyaos agent invoke estate-muse \
  '杭州亚运村二手房 500 条选题' \
  --listen http://127.0.0.1:7474 \
  --key sk-soya-dev-local \
  --artifact xlsx \
  --schema topics.v1 \
  --output ./dist/trial/topics.xlsx
```

Open `dist/trial/topics.xlsx` in Excel, WPS, or Numbers. Confirm that the
`Topics` sheet has one header row plus 500 data rows, Chinese text displays
correctly, filters work, and the header remains frozen while scrolling.

### 4. Generate content from one saved row

The initial invocation has already persisted `row-17`; the caller does not
need to resend its topic fields:

```bash
curl http://127.0.0.1:7474/v1/agents/estate-muse/actions/generate_post \
  -H 'Authorization: Bearer sk-soya-dev-local' \
  -H 'Content-Type: application/json' \
  -d '{
        "row_id": "row-17",
        "payload": {"city": "杭州"}
      }'
```

SoyaOS merges action-specific options such as `city` with the saved row. The
saved title, dimension, angle, and hook override same-named caller fields, so
an action cannot silently replace the original workbook context.

Use `generate_video` instead of `generate_post` to produce the short-video
script. Each action manifest has a 60-second budget.

### 5. Verify restart persistence manually

1. Stop terminal A with `Ctrl+C`.
2. Run the exact same `soyaos start` command with the same `--data-dir`.
3. Do **not** rebuild, redeploy, or regenerate the Excel workbook.
4. Repeat the row-17 `curl` command.
5. Confirm the action still uses row 17's original topic.

For the two-week author trial, follow the beginner-friendly
[Chinese trial guide](./TRIAL_GUIDE.zh-CN.md) and create one feedback file per
session from [the template](./feedback/session-template.md).

## Verification

The E2E module imports the sibling SoyaOS source modules through local
`replace` directives:

```bash
make e2e
```

The suite runs with the race detector and covers:

- `production_binary_test.go` — real binary build, Pack validation/build/deploy,
  500-row XLSX generation, row action, process restart, and saved-row reuse;
- `kv_state_test.go` — bbolt persistence, MVCC/CAS concurrency, row isolation,
  prefix listing, deletion, restart, and 500-row scale;
- `rowjwt_e2e_test.go` — positive and negative row-token authorization over a
  real HTTP gateway, including substitution, expiry, forgery, and escalation.
  The persistent signer wired by APP-1072 is also verified across process
  restarts: matching tokens survive while mismatched tokens fail;
- `manifest_test.go` — authoritative manifest validation and declared surfaces;
- `xlsx_compat_test.go` — 500-row Excel structure, Chinese content, filters,
  validation, hyperlinks, numeric formats, and compatibility sample export.

The production-binary test uses a deterministic local mock only for the
external model endpoint. All SoyaOS, Pack, persistence, HTTP, CLI, and XLSX
paths are the production implementations.

## Repository layout

```text
example-estate-muse/
├── soyapack.yaml
├── README.md
├── TRIAL_GUIDE.zh-CN.md
├── prompts/
│   ├── collect.md
│   ├── expand.md
│   ├── dedupe.md
│   ├── generate_post.md
│   └── generate_video.md
├── templates/
│   ├── topics.xlsx.tmpl
│   └── topics.html.tmpl
├── examples/
├── feedback/
└── e2e/
```

## Manifest highlights

```yaml
spec_version: soyapack.v0
kind: Agent
name: estate-muse
determinism: stateful
expose:
  virtual_model_id: soya:estate-muse
artifacts:
  - { kind: xlsx, schema: topics.v1 }
  - { kind: html, schema: topics-table.v1 }
  - { kind: markdown, schema: wechat_post.v1 }
  - { kind: markdown, schema: video_script.v1 }
actions:
  - { id: generate_post, on: per_row, handler: prompts/generate_post.md, timeout: 60s }
  - { id: generate_video, on: per_row, handler: prompts/generate_video.md, timeout: 60s }
state:
  scope: agent
  store: kv
```

The manifest is the contract. Anything rejected by `pkg/soyapack.Validate`
must not be accepted by the runtime.

## Model configuration

EstateMuse's editorial quality depends heavily on the upstream model. The Pack
does not pin a provider, so the operator selects one with:

| Variable | Meaning | Example |
|---|---|---|
| `SOYA_MODEL_API_KEY` | Provider API key | secret; never commit |
| `SOYA_MODEL_BASE_URL` | OpenAI-compatible base URL | `https://api.openai.com/v1` |
| `SOYA_MODEL_DEFAULT` | Provider model ID | `gpt-4o` |

For a cheap smoke test, use a smaller model. For the two-week editorial trial,
use one stable Chinese-capable model for the entire cohort so model changes do
not contaminate author feedback.

## Per-row security and state

- Standard `sk-soya` keys may invoke any action allowed by their scopes.
- A row JWT is bound to one `(agent_slug, action_id, row_id)` tuple and expires
  within 24 hours.
- Changing the row, action, agent, signature, or expiry yields `401` before the
  external model is called.
- Workbook row context is stored under the Agent's persistent state partition.
- Caller-supplied fields cannot overwrite the saved workbook row.

## Alpha limitations

- The CLI renders `topics.v1` directly to XLSX; automatic simultaneous HTML
  companion generation is not wired yet.
- The manifest declares `tool.originality_check`, but current output quality
  relies on the prompt's self-review until runtime tool orchestration lands.
- Real editorial quality, originality, and usefulness still require the
  five-author, two-week trial tracked separately from the technical E2E.
- APIs, state schema, commands, and output contracts may change before release.

## Status

| Milestone | Status |
|---|---|
| Manifest, prompt chain, actions, templates | Complete |
| Pack validation, build, and Solo deploy | Complete |
| Real 500-row XLSX through production CLI | Complete |
| Agent state and row context survive restart | Complete |
| Real post/video action under 60 seconds | Technically verified with deterministic model mock |
| Automatic HTML companion rendering | Pending |
| Runtime originality tool orchestration | Pending |
| Five authors × two-week editorial acceptance | Pending human trial |

## License

MIT — see [LICENSE](./LICENSE).
