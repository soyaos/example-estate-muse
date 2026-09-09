# Trial feedback

This directory stores anonymized, text-only evidence for the owner's two-week
EstateMuse self-trial (`EM-01`). On 2026-09-09 the owner explicitly replaced the
five-external-author requirement with their own real needs. This is personal
workflow validation, not evidence of external-user adoption.

> Never commit API keys, row tokens, customer details, private listings,
> unredacted screenshots, raw audio, or unpublished commercial data.

Create one anonymous participant directory (`EM-01`) and one
Markdown file per session. Copy `session-template.md`; do not overwrite earlier
sessions. Raw recordings and screenshots stay in the approved private evidence
store, referenced here only by an opaque `EV-*` evidence ID.

Keep at least 14 calendar days and 3 real-use sessions, at least one 500-row
XLSX opened and edited by the owner, and both `generate_post` and
`generate_video` across the trial. Quality sampling, latency, issue review and
privacy gates remain unchanged. AI cannot attest to the owner's real use.

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
