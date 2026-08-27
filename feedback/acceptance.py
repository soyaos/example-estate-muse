#!/usr/bin/env python3
"""Validate and summarize anonymized EstateMuse trial evidence.

The validator deliberately accepts structured, low-entropy evidence only. Raw
prompts, generated articles, transcripts, screenshots, credentials and contact
details belong outside Git and must never be used to make this report pass.
"""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import math
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
from typing import Any, Iterable


PARTICIPANT_SCHEMA = "estate-muse-participant.v1"
TECHNICAL_SCHEMA = "estate-muse-technical-baseline.v1"
ISSUE_SCHEMA = "estate-muse-issue-register.v1"
EXPECTED_PARTICIPANTS = tuple(f"EM-{index:02d}" for index in range(1, 6))
PROFILE_VALUES = {
    "公众号作者",
    "视频号作者",
    "小红书作者",
    "抖音作者",
    "经纪公司内容运营",
    "房产内容编辑",
}
PLATFORM_VALUES = {"公众号", "视频号", "小红书", "抖音", "其他"}
DESKTOP_APP_VALUES = {"Microsoft Excel", "WPS", "Apple Numbers", "LibreOffice", "其他"}
OUTCOME_VALUES = {"继续使用", "修改后使用", "不会使用"}
ACTION_VALUES = {"generate_post", "generate_video"}
SEVERITY_VALUES = {"P0", "P1", "P2"}
ISSUE_STATUS_VALUES = {"fixed_and_retested", "tracked"}
FORBIDDEN_KEYS = {
    "name",
    "real_name",
    "email",
    "phone",
    "wechat",
    "address",
    "contact",
    "api_key",
    "token",
    "password",
    "secret",
    "prompt",
    "input",
    "article",
    "content",
    "transcript",
    "raw_audio",
    "screenshot",
    "listing",
    "customer",
}
SENSITIVE_PATTERNS = {
    "email address": re.compile(r"\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b", re.IGNORECASE),
    "mainland China phone number": re.compile(r"(?<!\d)1[3-9]\d{9}(?!\d)"),
    "Chinese resident ID": re.compile(r"(?<!\d)\d{17}[0-9Xx](?!\d)"),
    "private key": re.compile(r"-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----"),
    "authorization credential": re.compile(r"\b(?:Bearer|Basic)\s+[A-Za-z0-9+/_.=-]{8,}", re.IGNORECASE),
    "API token": re.compile(r"\b(?:sk|gh[opusr]|github_pat)[-_][A-Za-z0-9_-]{12,}\b", re.IGNORECASE),
    "JWT": re.compile(r"\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b"),
    "absolute home path": re.compile(r"(?:/Users|/home)/[^/\s]+/"),
}
OPAQUE_EVIDENCE_ID = re.compile(r"^EV-[A-Z0-9][A-Z0-9-]{5,63}$")
ROW_REFERENCE = re.compile(r"^(?:row-[A-Za-z0-9_-]{1,64}|sha256:[0-9a-f]{64})$")
SESSION_ID = re.compile(r"^EM-0[1-5]-S\d{2,3}$")
LINEAR_ISSUE = re.compile(r"^APP-\d+$")


class ValidationResult:
    def __init__(self) -> None:
        self.errors: list[str] = []
        self.participants: dict[str, dict[str, Any]] = {}
        self.technical: dict[str, Any] | None = None
        self.issue_register: dict[str, Any] | None = None

    @property
    def passed(self) -> bool:
        return not self.errors

    def add(self, message: str) -> None:
        if message not in self.errors:
            self.errors.append(message)


def _parse_date(value: Any, label: str, result: ValidationResult) -> dt.date | None:
    if not isinstance(value, str):
        result.add(f"{label}: must be an ISO date (YYYY-MM-DD)")
        return None
    try:
        return dt.date.fromisoformat(value)
    except ValueError:
        result.add(f"{label}: invalid ISO date {value!r}")
        return None


def _require_bool(data: dict[str, Any], key: str, label: str, result: ValidationResult) -> bool:
    value = data.get(key)
    if value is not True:
        result.add(f"{label}.{key}: must be true")
        return False
    return True


def _positive_int(value: Any) -> bool:
    return isinstance(value, int) and not isinstance(value, bool) and value > 0


def _reject_unknown(
    data: dict[str, Any],
    allowed: set[str],
    label: str,
    result: ValidationResult,
) -> None:
    for key in sorted(set(data) - allowed):
        result.add(f"{label}.{key}: unknown field (schema is closed)")


def _walk(value: Any, path: str = "$") -> Iterable[tuple[str, Any]]:
    yield path, value
    if isinstance(value, dict):
        for key, child in value.items():
            yield from _walk(child, f"{path}.{key}")
    elif isinstance(value, list):
        for index, child in enumerate(value):
            yield from _walk(child, f"{path}[{index}]")


def _privacy_scan(raw: str, data: Any, source: str, result: ValidationResult) -> None:
    for kind, pattern in SENSITIVE_PATTERNS.items():
        if pattern.search(raw):
            result.add(f"{source}: possible {kind}; remove it from Git evidence")
    for path, value in _walk(data):
        if isinstance(value, dict):
            for key in value:
                if key.lower() in FORBIDDEN_KEYS:
                    result.add(f"{source}:{path}.{key}: forbidden raw/private field")


def _load_json(path: Path, result: ValidationResult) -> dict[str, Any] | None:
    try:
        raw = path.read_text(encoding="utf-8")
        data = json.loads(raw)
    except (OSError, json.JSONDecodeError) as error:
        result.add(f"{path}: cannot read valid JSON: {error}")
        return None
    if not isinstance(data, dict):
        result.add(f"{path}: top-level JSON value must be an object")
        return None
    _privacy_scan(raw, data, str(path), result)
    return data


def _validate_action(
    action: Any,
    label: str,
    result: ValidationResult,
) -> str | None:
    if not isinstance(action, dict):
        result.add(f"{label}: action must be an object")
        return None
    _reject_unknown(
        action,
        {"action", "row_ref", "duration_ms", "success", "used_persisted_row"},
        label,
        result,
    )
    action_name = action.get("action")
    if action_name not in ACTION_VALUES:
        result.add(f"{label}.action: must be generate_post or generate_video")
    row_ref = action.get("row_ref")
    if not isinstance(row_ref, str) or not ROW_REFERENCE.fullmatch(row_ref):
        result.add(f"{label}.row_ref: must be an opaque row ID or SHA-256 reference")
    duration = action.get("duration_ms")
    if not _positive_int(duration):
        result.add(f"{label}.duration_ms: must be a positive integer")
    elif duration > 60_000:
        result.add(f"{label}.duration_ms: {duration} exceeds the 60000 ms hard limit")
    _require_bool(action, "success", label, result)
    _require_bool(action, "used_persisted_row", label, result)
    return action_name if action_name in ACTION_VALUES else None


def _validate_xlsx(xlsx: Any, label: str, result: ValidationResult) -> int | None:
    if not isinstance(xlsx, dict):
        result.add(f"{label}: xlsx must be an object")
        return None
    _reject_unknown(
        xlsx,
        {
            "row_count",
            "duration_ms",
            "desktop_app",
            "opened",
            "edited_successfully",
            "chinese_ok",
            "filters_ok",
            "freeze_header_ok",
        },
        label,
        result,
    )
    row_count = xlsx.get("row_count")
    if not isinstance(row_count, int) or isinstance(row_count, bool) or row_count < 500:
        result.add(f"{label}.row_count: must be at least 500")
    duration = xlsx.get("duration_ms")
    if not _positive_int(duration):
        result.add(f"{label}.duration_ms: must be a positive integer")
    elif duration > 300_000:
        result.add(f"{label}.duration_ms: {duration} exceeds the 300000 ms hard limit")
    if xlsx.get("desktop_app") not in DESKTOP_APP_VALUES:
        result.add(f"{label}.desktop_app: unsupported or missing spreadsheet application")
    for key in ("opened", "edited_successfully", "chinese_ok", "filters_ok", "freeze_header_ok"):
        _require_bool(xlsx, key, label, result)
    return duration if _positive_int(duration) else None


def _validate_quality(quality: Any, label: str, result: ValidationResult) -> None:
    if not isinstance(quality, dict):
        result.add(f"{label}: quality_summary must be an object")
        return
    _reject_unknown(
        quality,
        {
            "sampled_rows",
            "grade_counts",
            "duplicate_groups_count",
            "omitted_angles_count",
            "originality_samples",
            "originality_warning_accurate_count",
            "originality_false_positive_count",
            "originality_false_negative_count",
            "final_outcome",
            "confirmed_anonymous_quote",
        },
        label,
        result,
    )
    sampled = quality.get("sampled_rows")
    grades = quality.get("grade_counts")
    if not _positive_int(sampled):
        result.add(f"{label}.sampled_rows: must be a positive integer")
    if not isinstance(grades, dict) or any(
        not isinstance(grades.get(grade), int) or isinstance(grades.get(grade), bool) or grades.get(grade) < 0
        for grade in ("A", "B", "C")
    ):
        result.add(f"{label}.grade_counts: A, B and C must be non-negative integers")
    elif _positive_int(sampled) and sum(grades[grade] for grade in ("A", "B", "C")) != sampled:
        result.add(f"{label}.grade_counts: A+B+C must equal sampled_rows")
    for key in ("duplicate_groups_count", "omitted_angles_count"):
        value = quality.get(key)
        if not isinstance(value, int) or isinstance(value, bool) or value < 0:
            result.add(f"{label}.{key}: must be a non-negative integer")
    originality_samples = quality.get("originality_samples")
    originality_fields = (
        "originality_warning_accurate_count",
        "originality_false_positive_count",
        "originality_false_negative_count",
    )
    if not _positive_int(originality_samples):
        result.add(f"{label}.originality_samples: must be a positive integer")
    originality_counts: list[int] = []
    for key in originality_fields:
        value = quality.get(key)
        if not isinstance(value, int) or isinstance(value, bool) or value < 0:
            result.add(f"{label}.{key}: must be a non-negative integer")
        else:
            originality_counts.append(value)
    if _positive_int(originality_samples) and len(originality_counts) == len(originality_fields):
        if sum(originality_counts) != originality_samples:
            result.add(f"{label}: originality counts must add up to originality_samples")
    if quality.get("final_outcome") not in OUTCOME_VALUES:
        result.add(f"{label}.final_outcome: must be one of {sorted(OUTCOME_VALUES)}")
    quote = quality.get("confirmed_anonymous_quote")
    if not isinstance(quote, str) or not quote.strip():
        result.add(f"{label}.confirmed_anonymous_quote: required")
    elif len(quote) > 280:
        result.add(f"{label}.confirmed_anonymous_quote: must be at most 280 characters")
    elif re.search(r"(?:<!doctype\s+html|<script\b|```)", quote, re.IGNORECASE):
        result.add(f"{label}.confirmed_anonymous_quote: raw HTML and code blocks are forbidden")


def _validate_participant(
    data: dict[str, Any],
    source: Path,
    expected_id: str,
    result: ValidationResult,
) -> None:
    prefix = str(source)
    _reject_unknown(
        data,
        {
            "schema_version",
            "participant_id",
            "profile",
            "trial_started_on",
            "trial_ended_on",
            "attestation",
            "opaque_evidence_ids",
            "sessions",
            "quality_summary",
        },
        prefix,
        result,
    )
    if data.get("schema_version") != PARTICIPANT_SCHEMA:
        result.add(f"{prefix}.schema_version: must be {PARTICIPANT_SCHEMA}")
    participant_id = data.get("participant_id")
    if participant_id != expected_id:
        result.add(f"{prefix}.participant_id: must match filename and equal {expected_id}")
    if data.get("profile") not in PROFILE_VALUES:
        result.add(f"{prefix}.profile: must use a non-identifying profile enum")

    attestation = data.get("attestation")
    if not isinstance(attestation, dict):
        result.add(f"{prefix}.attestation: required")
    else:
        _reject_unknown(
            attestation,
            {
                "real_human",
                "real_estate_content_creator",
                "fourteen_day_trial_consented",
                "anonymous_feedback_confirmed",
                "coordinator_verified",
                "verified_on",
            },
            f"{prefix}.attestation",
            result,
        )
        for key in (
            "real_human",
            "real_estate_content_creator",
            "fourteen_day_trial_consented",
            "anonymous_feedback_confirmed",
            "coordinator_verified",
        ):
            _require_bool(attestation, key, f"{prefix}.attestation", result)
        _parse_date(attestation.get("verified_on"), f"{prefix}.attestation.verified_on", result)

    started = _parse_date(data.get("trial_started_on"), f"{prefix}.trial_started_on", result)
    ended = _parse_date(data.get("trial_ended_on"), f"{prefix}.trial_ended_on", result)
    if started and ended:
        coverage_days = (ended - started).days + 1
        if coverage_days < 14:
            result.add(f"{prefix}: trial covers {coverage_days} calendar days, need at least 14")

    evidence_ids = data.get("opaque_evidence_ids")
    if not isinstance(evidence_ids, list) or not evidence_ids:
        result.add(f"{prefix}.opaque_evidence_ids: at least one private-store evidence reference is required")
    else:
        for evidence_id in evidence_ids:
            if not isinstance(evidence_id, str) or not OPAQUE_EVIDENCE_ID.fullmatch(evidence_id):
                result.add(f"{prefix}.opaque_evidence_ids: {evidence_id!r} is not an opaque EV-* identifier")

    sessions = data.get("sessions")
    if not isinstance(sessions, list) or len(sessions) < 3:
        result.add(f"{prefix}.sessions: at least 3 real-use sessions are required")
        sessions = []
    seen_sessions: set[str] = set()
    qualifying_xlsx = 0
    action_names: set[str] = set()
    action_count = 0
    for index, session in enumerate(sessions):
        label = f"{prefix}.sessions[{index}]"
        if not isinstance(session, dict):
            result.add(f"{label}: session must be an object")
            continue
        _reject_unknown(
            session,
            {"session_id", "occurred_on", "target_platform", "xlsx", "actions"},
            label,
            result,
        )
        session_id = session.get("session_id")
        if not isinstance(session_id, str) or not SESSION_ID.fullmatch(session_id) or not session_id.startswith(expected_id + "-"):
            result.add(f"{label}.session_id: invalid or belongs to another participant")
        elif session_id in seen_sessions:
            result.add(f"{label}.session_id: duplicate {session_id}")
        else:
            seen_sessions.add(session_id)
        occurred = _parse_date(session.get("occurred_on"), f"{label}.occurred_on", result)
        if occurred and started and ended and not (started <= occurred <= ended):
            result.add(f"{label}.occurred_on: must fall inside the declared trial period")
        if session.get("target_platform") not in PLATFORM_VALUES:
            result.add(f"{label}.target_platform: unsupported or missing platform")
        if "xlsx" in session:
            _validate_xlsx(session["xlsx"], f"{label}.xlsx", result)
            qualifying_xlsx += 1
        actions = session.get("actions", [])
        if not isinstance(actions, list):
            result.add(f"{label}.actions: must be an array")
            continue
        for action_index, action in enumerate(actions):
            action_name = _validate_action(action, f"{label}.actions[{action_index}]", result)
            if action_name:
                action_names.add(action_name)
                action_count += 1
    if qualifying_xlsx < 1:
        result.add(f"{prefix}: at least one validated 500-row XLSX run is required")
    if action_count < 1:
        result.add(f"{prefix}: at least one successful per-row action is required")
    if not action_names:
        result.add(f"{prefix}: no recognized per-row action evidence found")

    _validate_quality(data.get("quality_summary"), f"{prefix}.quality_summary", result)


def _validate_technical(data: dict[str, Any], source: Path, result: ValidationResult) -> None:
    prefix = str(source)
    _reject_unknown(
        data,
        {"schema_version", "recorded_at", "repository_commit", "working_tree", "production_trial_path"},
        prefix,
        result,
    )
    if data.get("schema_version") != TECHNICAL_SCHEMA:
        result.add(f"{prefix}.schema_version: must be {TECHNICAL_SCHEMA}")
    if not isinstance(data.get("recorded_at"), str):
        result.add(f"{prefix}.recorded_at: required")
    commit = data.get("repository_commit")
    if not isinstance(commit, str) or not re.fullmatch(r"[0-9a-f]{40}", commit):
        result.add(f"{prefix}.repository_commit: must be a full Git SHA")
    working_tree = data.get("working_tree")
    if not isinstance(working_tree, dict):
        result.add(f"{prefix}.working_tree: required")
    else:
        _reject_unknown(working_tree, {"clean", "diff_sha256"}, f"{prefix}.working_tree", result)
        _require_bool(working_tree, "clean", f"{prefix}.working_tree", result)
        diff_sha = working_tree.get("diff_sha256")
        if not isinstance(diff_sha, str) or not re.fullmatch(r"[0-9a-f]{64}", diff_sha):
            result.add(f"{prefix}.working_tree.diff_sha256: must be a SHA-256")
    test = data.get("production_trial_path")
    if not isinstance(test, dict):
        result.add(f"{prefix}.production_trial_path: required")
        return
    _reject_unknown(
        test,
        {"test_name", "command", "external_model", "passed", "asserted_contracts"},
        f"{prefix}.production_trial_path",
        result,
    )
    _require_bool(test, "passed", f"{prefix}.production_trial_path", result)
    if test.get("test_name") != "TestProductionBinary_EstateMuseTrialPath":
        result.add(f"{prefix}.production_trial_path.test_name: unexpected test")
    if test.get("external_model") != "deterministic_local_mock":
        result.add(f"{prefix}.production_trial_path.external_model: must disclose the deterministic local mock")
    coverage = test.get("asserted_contracts")
    expected = {
        "500_rows_under_300000_ms",
        "per_row_action_under_60000_ms",
        "xlsx_reopens",
        "state_survives_process_restart",
        "persisted_row_cannot_be_overridden",
    }
    if not isinstance(coverage, list) or not expected.issubset(set(coverage)):
        result.add(f"{prefix}.production_trial_path.asserted_contracts: missing required automatic contracts")


def _validate_issue_register(data: dict[str, Any], source: Path, result: ValidationResult) -> None:
    prefix = str(source)
    _reject_unknown(data, {"schema_version", "review_completed", "issues"}, prefix, result)
    if data.get("schema_version") != ISSUE_SCHEMA:
        result.add(f"{prefix}.schema_version: must be {ISSUE_SCHEMA}")
    _require_bool(data, "review_completed", prefix, result)
    issues = data.get("issues")
    if not isinstance(issues, list):
        result.add(f"{prefix}.issues: must be an array")
        return
    seen: set[str] = set()
    for index, issue in enumerate(issues):
        label = f"{prefix}.issues[{index}]"
        if not isinstance(issue, dict):
            result.add(f"{label}: issue must be an object")
            continue
        _reject_unknown(issue, {"code", "severity", "status", "linear_issue"}, label, result)
        code = issue.get("code")
        if not isinstance(code, str) or not re.fullmatch(r"EM-P[0-2]-\d{3}", code):
            result.add(f"{label}.code: must match EM-P<severity>-NNN")
        elif code in seen:
            result.add(f"{label}.code: duplicate {code}")
        else:
            seen.add(code)
        severity = issue.get("severity")
        if severity not in SEVERITY_VALUES:
            result.add(f"{label}.severity: must be P0, P1 or P2")
        status = issue.get("status")
        if status not in ISSUE_STATUS_VALUES:
            result.add(f"{label}.status: must be fixed_and_retested or tracked")
        linear_issue = issue.get("linear_issue")
        if not isinstance(linear_issue, str) or not LINEAR_ISSUE.fullmatch(linear_issue):
            result.add(f"{label}.linear_issue: must be an APP-* issue identifier")
        if severity in {"P0", "P1"} and status != "fixed_and_retested":
            result.add(f"{label}: {severity} must be fixed_and_retested")
        if severity == "P2" and status != "tracked":
            result.add(f"{label}: P2 must be tracked in its Linear issue")


def validate_evidence(evidence_dir: Path) -> ValidationResult:
    result = ValidationResult()
    participants_dir = evidence_dir / "participants"
    for participant_id in EXPECTED_PARTICIPANTS:
        path = participants_dir / f"{participant_id}.json"
        if not path.is_file():
            result.add(f"{path}: missing real-participant evidence")
            continue
        data = _load_json(path, result)
        if data is not None:
            result.participants[participant_id] = data
            _validate_participant(data, path, participant_id, result)

    unexpected = sorted(
        path.name
        for path in participants_dir.glob("*.json")
        if path.stem not in EXPECTED_PARTICIPANTS
    ) if participants_dir.is_dir() else []
    if unexpected:
        result.add(f"{participants_dir}: unexpected participant files: {', '.join(unexpected)}")

    technical_path = evidence_dir / "technical-baseline.json"
    if not technical_path.is_file():
        result.add(f"{technical_path}: missing automatic production-path evidence")
    else:
        data = _load_json(technical_path, result)
        if data is not None:
            result.technical = data
            _validate_technical(data, technical_path, result)

    register_path = evidence_dir / "issue-register.json"
    if not register_path.is_file():
        result.add(f"{register_path}: missing P0/P1/P2 review register")
    else:
        data = _load_json(register_path, result)
        if data is not None:
            result.issue_register = data
            _validate_issue_register(data, register_path, result)

    action_types = {
        action.get("action")
        for participant in result.participants.values()
        for session in participant.get("sessions", [])
        if isinstance(session, dict)
        for action in session.get("actions", [])
        if isinstance(action, dict)
    }
    for action_name in sorted(ACTION_VALUES - action_types):
        result.add(f"trial-wide evidence: no successful {action_name} action recorded")
    return result


def _percentile(values: list[int], percentile: float) -> str:
    if not values:
        return "—"
    ordered = sorted(values)
    index = max(0, math.ceil(len(ordered) * percentile) - 1)
    return f"{ordered[index]} ms"


def _escape(value: Any) -> str:
    return str(value).replace("|", "\\|").replace("\n", " ").strip()


def _display_error(error: str, evidence_dir: Path) -> str:
    return error.replace(str(evidence_dir), "feedback/evidence")


def _participant_metrics(participant: dict[str, Any]) -> tuple[int, int, list[int], list[int]]:
    try:
        started = dt.date.fromisoformat(participant["trial_started_on"])
        ended = dt.date.fromisoformat(participant["trial_ended_on"])
        coverage_days = max(0, (ended - started).days + 1)
    except (KeyError, TypeError, ValueError):
        coverage_days = 0
    sessions = participant.get("sessions", [])
    if not isinstance(sessions, list):
        sessions = []
    xlsx_durations = [
        session["xlsx"]["duration_ms"]
        for session in sessions
        if isinstance(session, dict) and isinstance(session.get("xlsx"), dict)
        and _positive_int(session["xlsx"].get("duration_ms"))
    ]
    action_durations = [
        action["duration_ms"]
        for session in sessions
        if isinstance(session, dict)
        for action in session.get("actions", [])
        if isinstance(action, dict) and _positive_int(action.get("duration_ms"))
    ]
    return coverage_days, len(sessions), xlsx_durations, action_durations


def render_report(result: ValidationResult, evidence_dir: Path, as_of: str) -> str:
    status = "PASS" if result.passed else "BLOCKED"
    all_xlsx: list[int] = []
    all_actions: list[int] = []
    total_sessions = 0
    participant_rows: list[str] = []
    summaries: list[str] = []
    for participant_id in EXPECTED_PARTICIPANTS:
        participant = result.participants.get(participant_id)
        if not participant:
            participant_rows.append(f"| {participant_id} | — | 0 | 0 | 0 | 缺少证据 |")
            continue
        days, session_count, xlsx_durations, action_durations = _participant_metrics(participant)
        all_xlsx.extend(xlsx_durations)
        all_actions.extend(action_durations)
        total_sessions += session_count
        outcome = participant.get("quality_summary", {}).get("final_outcome", "—")
        participant_rows.append(
            f"| {participant_id} | {_escape(participant.get('profile', '—'))} | {days} | "
            f"{session_count} | {len(action_durations)} | {_escape(outcome)} |"
        )
        quality = participant.get("quality_summary", {})
        if not isinstance(quality, dict):
            quality = {}
        grades = quality.get("grade_counts", {})
        summaries.extend([
            f"### {participant_id}",
            "",
            f"- 画像：{_escape(participant.get('profile', '—'))}",
            f"- 抽样：A {grades.get('A', 0)} / B {grades.get('B', 0)} / C {grades.get('C', 0)}",
            f"- 重复组 / 遗漏角度：{quality.get('duplicate_groups_count', 0)} / {quality.get('omitted_angles_count', 0)}",
            f"- 原创度提示抽样：{quality.get('originality_samples', 0)}（准确 {quality.get('originality_warning_accurate_count', 0)} / 误报 {quality.get('originality_false_positive_count', 0)} / 漏报 {quality.get('originality_false_negative_count', 0)}）",
            f"- 最终选择：{_escape(outcome)}",
            f"- 经确认匿名原话：> {_escape(quality.get('confirmed_anonymous_quote', '—'))}",
            "",
        ])

    issues = (result.issue_register or {}).get("issues", [])
    issue_rows = [
        f"| {_escape(issue.get('code', '—'))} | {_escape(issue.get('severity', '—'))} | "
        f"{_escape(issue.get('status', '—'))} | {_escape(issue.get('linear_issue', '—'))} |"
        for issue in issues if isinstance(issue, dict)
    ]
    if not issue_rows:
        issue_rows.append("| — | — | — | — |")

    blockers = [f"- [ ] {_escape(_display_error(error, evidence_dir))}" for error in result.errors]
    if not blockers:
        blockers = ["- [x] 无阻塞项"]
    technical_passed = bool(
        result.technical
        and isinstance(result.technical.get("production_trial_path"), dict)
        and result.technical["production_trial_path"].get("passed") is True
    )

    lines = [
        "# EstateMuse v0.1.0 Alpha 两周试用验收报告",
        "",
        f"> 结论：**{status}**。本报告仅汇总匿名结构化证据，不包含身份、联系方式、原始输入或生成内容正文。",
        "",
        f"- 报告日期：{as_of}",
        "- 证据目录：`feedback/evidence`",
        f"- 真人参与者：{len(result.participants)} / 5",
        f"- 会话总数：{total_sessions}",
        f"- 自动生产链路：{'通过' if technical_passed else '未通过或缺失'}",
        "",
        "## 硬门槛",
        "",
        "| 指标 | 结果 |",
        "| --- | --- |",
        f"| 5 名真实房产作者 × 至少 14 天 | {'通过' if len(result.participants) == 5 and result.passed else '未满足'} |",
        f"| 每人至少 3 次使用、一次 500 行 XLSX、一次逐行 Action | {'通过' if result.passed else '未满足'} |",
        f"| 500 行生成 ≤ 300000 ms | p50 {_percentile(all_xlsx, 0.50)}；p95 {_percentile(all_xlsx, 0.95)} |",
        f"| 图文/视频 Action ≤ 60000 ms | p50 {_percentile(all_actions, 0.50)}；p95 {_percentile(all_actions, 0.95)} |",
        f"| 跨进程重启状态保留 | {'通过' if technical_passed else '未满足'} |",
        "",
        "## 参与者覆盖",
        "",
        "| 编号 | 画像 | 覆盖天数 | 会话 | Action | 最终选择 |",
        "| --- | --- | ---: | ---: | ---: | --- |",
        *participant_rows,
        "",
        "## 匿名摘要",
        "",
    ]
    if summaries:
        lines.extend(summaries)
    else:
        lines.extend(["尚无真实作者反馈；不生成替代性评价。", ""])
    lines.extend([
        "## P0 / P1 / P2",
        "",
        "| 编号 | 严重度 | 状态 | Linear |",
        "| --- | --- | --- | --- |",
        *issue_rows,
        "",
        "## 阻塞项",
        "",
        *blockers,
        "",
        "## 复现命令",
        "",
        "```bash",
        "python3 feedback/acceptance.py verify",
        "```",
        "",
        "只有该命令返回 0 时，APP-1701 和 APP-506 才具备关闭条件。自动测试通过不能替代 5 名真人的两周试用。",
        "",
    ])
    return "\n".join(lines)


def _atomic_write(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(
        mode="w",
        encoding="utf-8",
        dir=path.parent,
        prefix=path.name + ".",
        suffix=".tmp",
        delete=False,
    ) as handle:
        handle.write(content)
        temp_name = handle.name
    os.replace(temp_name, path)


def collect_technical(repo_root: Path, output: Path) -> int:
    command = [
        "go",
        "test",
        "-count=1",
        "-run",
        "^TestProductionBinary_EstateMuseTrialPath$",
        "-v",
        "./...",
    ]
    completed = subprocess.run(command, cwd=repo_root / "e2e", check=False)
    sha = subprocess.run(
        ["git", "rev-parse", "HEAD"],
        cwd=repo_root,
        check=True,
        capture_output=True,
        text=True,
    ).stdout.strip()
    diff = subprocess.run(
        ["git", "diff", "--binary", "HEAD"],
        cwd=repo_root,
        check=True,
        capture_output=True,
    ).stdout
    untracked = subprocess.run(
        ["git", "ls-files", "--others", "--exclude-standard"],
        cwd=repo_root,
        check=True,
        capture_output=True,
        text=True,
    ).stdout.splitlines()
    fingerprint = hashlib.sha256()
    fingerprint.update(diff)
    for relative in sorted(untracked):
        path = repo_root / relative
        if not path.is_file():
            continue
        fingerprint.update(relative.encode("utf-8"))
        fingerprint.update(b"\0")
        fingerprint.update(path.read_bytes())
        fingerprint.update(b"\0")
    evidence = {
        "schema_version": TECHNICAL_SCHEMA,
        "recorded_at": dt.datetime.now(dt.timezone.utc).isoformat(timespec="seconds"),
        "repository_commit": sha,
        "working_tree": {
            "clean": not diff and not untracked,
            "diff_sha256": fingerprint.hexdigest(),
        },
        "production_trial_path": {
            "test_name": "TestProductionBinary_EstateMuseTrialPath",
            "command": "cd e2e && " + " ".join(command),
            "external_model": "deterministic_local_mock",
            "passed": completed.returncode == 0,
            "asserted_contracts": [
                "500_rows_under_300000_ms",
                "per_row_action_under_60000_ms",
                "xlsx_reopens",
                "state_survives_process_restart",
                "persisted_row_cannot_be_overridden",
            ],
        },
    }
    _atomic_write(output, json.dumps(evidence, ensure_ascii=False, indent=2) + "\n")
    print(f"technical evidence written: {output}")
    return completed.returncode


def main(argv: list[str] | None = None) -> int:
    script = Path(__file__).resolve()
    repo_root = script.parent.parent
    default_evidence = script.parent / "evidence"
    default_report = script.parent / "estate-muse-v0.1.0-alpha.log.md"

    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)
    for name in ("report", "verify"):
        command = subparsers.add_parser(name)
        command.add_argument("--evidence-dir", type=Path, default=default_evidence)
        command.add_argument("--report", type=Path, default=default_report)
        command.add_argument("--as-of", default=dt.date.today().isoformat())
    collect = subparsers.add_parser("collect-technical")
    collect.add_argument("--output", type=Path, default=default_evidence / "technical-baseline.json")

    args = parser.parse_args(argv)
    if args.command == "collect-technical":
        return collect_technical(repo_root, args.output)

    result = validate_evidence(args.evidence_dir)
    report = render_report(result, args.evidence_dir, args.as_of)
    _privacy_scan(report, {}, str(args.report), result)
    if result.errors and "\n".join(result.errors) not in report:
        report = render_report(result, args.evidence_dir, args.as_of)
    _atomic_write(args.report, report)
    print(f"{args.command}: {'PASS' if result.passed else 'BLOCKED'}")
    print(f"report: {args.report}")
    for error in result.errors:
        print(f"- {error}")
    return 0 if args.command == "report" or result.passed else 1


if __name__ == "__main__":
    raise SystemExit(main())
