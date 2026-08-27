# APP-1701 anonymous evidence

This directory is the machine-readable input for the EstateMuse two-week
acceptance report. It is intentionally narrower than the private trial notes.

Never place names, contact details, credentials, raw prompts, generated article
bodies, transcripts, recordings or screenshots here. Keep those in the approved
private store and use only opaque `EV-*` references in participant evidence.

## Layout

```text
evidence/
├── participants/
│   ├── EM-01.json
│   └── ... EM-05.json
├── issue-register.json
└── technical-baseline.json
```

Copy `templates/participant.template.json` once for each real participant. The
coordinator must populate it from real sessions; AI must not synthesize missing
attestations, dates, quality scores, outcomes or quotes.

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
