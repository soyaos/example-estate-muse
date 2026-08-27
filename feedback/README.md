# Trial feedback

This directory stores anonymized, text-only evidence for the five-author,
two-week EstateMuse trial.

> Never commit API keys, row tokens, customer details, private listings,
> unredacted screenshots, raw audio, or unpublished commercial data.

Create one directory per anonymous participant (`EM-01` … `EM-05`) and one
Markdown file per session. Copy `session-template.md`; do not overwrite earlier
sessions. Raw recordings and screenshots stay in the approved private evidence
store, referenced here only by an opaque `EV-*` evidence ID.

The session notes are working material. Final acceptance uses the deliberately
narrow JSON contract in [`evidence/`](./evidence/), so identities, credentials,
raw prompts and generated content cannot accidentally flow into the report.

```bash
# Refresh automatic state/XLSX/action proof.
python3 feedback/acceptance.py collect-technical

# Generate a truthful progress report even while human evidence is pending.
python3 feedback/acceptance.py report

# Final gate: only exit 0 when every APP-1701 hard requirement is met.
python3 feedback/acceptance.py verify
```

The operating procedure and minimum completion requirements are in
[`TRIAL_GUIDE.zh-CN.md`](../TRIAL_GUIDE.zh-CN.md).
