# APP-1701 anonymous evidence

This directory is the machine-readable input for the EstateMuse two-week
acceptance report. It is intentionally narrower than the private trial notes.

Scope: `owner_self_trial`, decided by the owner on **2026-09-09**. Only `EM-01`
is required; the earlier five external authors are no longer an acceptance
condition. A passing result covers the owner's real workflow only, not an
external-user trial. Participant schema v2 records this scope explicitly;
legacy v1 records are not silently reinterpreted.

Never place names, contact details, credentials, raw prompts, generated article
bodies, transcripts, recordings or screenshots here. Keep those in the approved
private store and use only opaque `EV-*` references in participant evidence.

## Layout

```text
evidence/
├── participants/
│   └── EM-01.json
├── issue-register.json
└── technical-baseline.json
```

Copy `templates/participant.template.json` to `participants/EM-01.json` only
when collecting the owner's real evidence. The template intentionally contains
null dates, false attestations and empty sessions: copying it does not pass.
Populate it from actual sessions with the owner's confirmation; AI must not
synthesize missing attestations, dates, quality scores, outcomes or quotes.
`profile` may be `本人真实需求试用`; professional creator status and an external
coordinator are not required. `owner_confirmed` refers to confirmation by the
owner, not an AI assertion.

The unchanged gates require at least 14 calendar days, 3 real-use sessions,
one 500-row XLSX run opened and edited in a desktop spreadsheet app, and both
`generate_post` and `generate_video` across the trial. Use the session/action
field definitions in `schemas/participant.schema.json`; a session does not need
3 actions. Preserve latency, persisted-row, quality sampling, P0/P1 retest,
P2 tracking and privacy checks. Automatic mock evidence remains separate and
cannot stand in for personal use or quality judgments.

Run the automatic production-path proof before final acceptance:

```bash
python3 feedback/acceptance.py collect-technical
```

Generate a non-passing progress report at any time:

```bash
python3 feedback/acceptance.py report
```

The strict acceptance gate returns non-zero until every hard requirement is
met:

```bash
python3 feedback/acceptance.py verify
```
