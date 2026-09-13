from __future__ import annotations

import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest


MODULE_PATH = Path(__file__).resolve().parents[1] / "acceptance.py"
SPEC = importlib.util.spec_from_file_location("estate_muse_acceptance", MODULE_PATH)
acceptance = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(acceptance)


def participant(index: int) -> dict:
    participant_id = f"EM-{index:02d}"
    post_first = index % 2 == 1
    return {
        "schema_version": acceptance.PARTICIPANT_SCHEMA,
        "trial_scope": acceptance.TRIAL_SCOPE,
        "scope_decided_on": acceptance.SCOPE_DECIDED_ON,
        "participant_id": participant_id,
        "profile": "公众号作者",
        "trial_started_on": "2026-09-01",
        "trial_ended_on": "2026-09-14",
        "attestation": {
            "real_human": True,
            "project_owner": True,
            "real_personal_need": True,
            "fourteen_day_trial_consented": True,
            "anonymous_feedback_confirmed": True,
            "owner_confirmed": True,
            "verified_on": "2026-09-14",
        },
        "opaque_evidence_ids": [f"EV-EM{index:02d}-TRIAL"],
        "sessions": [
            {
                "session_id": f"{participant_id}-S01",
                "occurred_on": "2026-09-01",
                "target_platform": "公众号",
                "xlsx": {
                    "row_count": 500,
                    "duration_ms": 120000 + index,
                    "desktop_app": "Microsoft Excel",
                    "opened": True,
                    "edited_successfully": True,
                    "chinese_ok": True,
                    "filters_ok": True,
                    "freeze_header_ok": True,
                },
                "actions": [{
                    "action": "generate_post" if post_first else "generate_video",
                    "row_ref": f"row-{index}",
                    "duration_ms": 30000 + index,
                    "success": True,
                    "used_persisted_row": True,
                }],
            },
            {
                "session_id": f"{participant_id}-S02",
                "occurred_on": "2026-09-07",
                "target_platform": "视频号",
                "actions": [{
                    "action": "generate_video" if post_first else "generate_post",
                    "row_ref": f"row-{index + 10}",
                    "duration_ms": 32000 + index,
                    "success": True,
                    "used_persisted_row": True,
                }],
            },
            {
                "session_id": f"{participant_id}-S03",
                "occurred_on": "2026-09-14",
                "target_platform": "公众号",
                "actions": [],
            },
        ],
        "quality_summary": {
            "sampled_rows": 60,
            "grade_counts": {"A": 20, "B": 30, "C": 10},
            "duplicate_groups_count": 3,
            "omitted_angles_count": 2,
            "originality_samples": 10,
            "originality_warning_accurate_count": 7,
            "originality_false_positive_count": 2,
            "originality_false_negative_count": 1,
            "final_outcome": "修改后使用",
            "confirmed_anonymous_quote": "选题筛选更快，但发布前仍需要事实核验。",
        },
    }


class AcceptanceTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        (self.root / "participants").mkdir()
        self.write_complete_evidence()

    def tearDown(self) -> None:
        self.temp.cleanup()

    def write_json(self, relative: str, data: dict) -> None:
        path = self.root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(data, ensure_ascii=False), encoding="utf-8")

    def write_complete_evidence(self) -> None:
        for index in range(1, 2):
            self.write_json(f"participants/EM-{index:02d}.json", participant(index))
        self.write_json("technical-baseline.json", {
            "schema_version": acceptance.TECHNICAL_SCHEMA,
            "recorded_at": "2026-09-14T12:00:00+00:00",
            "repository_commit": "a" * 40,
            "working_tree": {"clean": True, "diff_sha256": "b" * 64},
            "production_trial_path": {
                "test_name": "TestProductionBinary_EstateMuseTrialPath",
                "command": "cd e2e && go test",
                "external_model": "deterministic_local_mock",
                "passed": True,
                "asserted_contracts": [
                    "500_rows_under_300000_ms",
                    "per_row_action_under_60000_ms",
                    "xlsx_reopens",
                    "state_survives_process_restart",
                    "persisted_row_cannot_be_overridden",
                ],
            },
        })
        self.write_json("issue-register.json", {
            "schema_version": acceptance.ISSUE_SCHEMA,
            "review_completed": True,
            "issues": [
                {"code": "EM-P1-001", "severity": "P1", "status": "fixed_and_retested", "linear_issue": "APP-2001"},
                {"code": "EM-P2-001", "severity": "P2", "status": "tracked", "linear_issue": "APP-2002"},
            ],
        })

    def test_complete_real_evidence_passes_and_report_is_anonymous(self) -> None:
        result = acceptance.validate_evidence(self.root)
        self.assertTrue(result.passed, result.errors)
        report = acceptance.render_report(result, self.root, "2026-09-14")
        self.assertIn("**PASS**", report)
        self.assertIn("真人参与者：1 / 1", report)
        self.assertIn("范围决定日期：2026-09-09", report)
        self.assertIn("不代表外部用户验证", report)
        self.assertNotIn("sk-", report)

    def test_missing_participant_is_a_hard_blocker(self) -> None:
        (self.root / "participants" / "EM-01.json").unlink()
        result = acceptance.validate_evidence(self.root)
        self.assertFalse(result.passed)
        self.assertTrue(any("EM-01.json: missing" in error for error in result.errors))

    def test_fourteen_days_is_inclusive(self) -> None:
        data = participant(1)
        data["trial_ended_on"] = "2026-09-13"
        self.write_json("participants/EM-01.json", data)
        result = acceptance.validate_evidence(self.root)
        self.assertTrue(any("covers 13 calendar days" in error for error in result.errors))

    def test_latency_limits_block_acceptance(self) -> None:
        data = participant(1)
        data["sessions"][0]["xlsx"]["duration_ms"] = 300001
        data["sessions"][0]["actions"][0]["duration_ms"] = 60001
        self.write_json("participants/EM-01.json", data)
        result = acceptance.validate_evidence(self.root)
        self.assertTrue(any("300001 exceeds" in error for error in result.errors))
        self.assertTrue(any("60001 exceeds" in error for error in result.errors))

    def test_pii_and_raw_content_fields_are_rejected(self) -> None:
        data = participant(1)
        data["email"] = "person@example.com"
        data["sessions"][0]["input"] = "未公开楼盘正文"
        self.write_json("participants/EM-01.json", data)
        result = acceptance.validate_evidence(self.root)
        self.assertTrue(any("possible email address" in error for error in result.errors))
        self.assertTrue(any("forbidden raw/private field" in error for error in result.errors))

    def test_secret_like_values_are_rejected(self) -> None:
        data = participant(1)
        data["quality_summary"]["confirmed_anonymous_quote"] = "sk-soya-this-must-never-enter-evidence"
        self.write_json("participants/EM-01.json", data)
        result = acceptance.validate_evidence(self.root)
        self.assertTrue(any("possible API token" in error for error in result.errors))

    def test_open_p1_or_invalid_p2_status_is_rejected(self) -> None:
        register = {
            "schema_version": acceptance.ISSUE_SCHEMA,
            "review_completed": True,
            "issues": [
                {"code": "EM-P1-001", "severity": "P1", "status": "tracked", "linear_issue": "APP-2001"},
                {"code": "EM-P2-001", "severity": "P2", "status": "ignored", "linear_issue": "APP-2002"},
            ],
        }
        self.write_json("issue-register.json", register)
        result = acceptance.validate_evidence(self.root)
        self.assertTrue(any("P1 must be fixed_and_retested" in error for error in result.errors))
        self.assertTrue(any("status: must be fixed_and_retested or tracked" in error for error in result.errors))

    def test_p2_accepts_tracked_or_fixed_but_always_requires_linear_issue(self) -> None:
        register = json.loads((self.root / "issue-register.json").read_text())
        for status in ("tracked", "fixed_and_retested"):
            for linear_issue in ("APP-2002", None, ""):
                with self.subTest(status=status, linear_issue=linear_issue):
                    register["issues"][1]["status"] = status
                    register["issues"][1]["linear_issue"] = linear_issue
                    self.write_json("issue-register.json", register)
                    result = acceptance.validate_evidence(self.root)
                    if linear_issue:
                        self.assertTrue(result.passed, result.errors)
                    else:
                        self.assertFalse(result.passed)
                        self.assertTrue(any("linear_issue: must be an APP-*" in error for error in result.errors))

    def test_report_preserves_blocked_truth_without_inventing_feedback(self) -> None:
        for path in (self.root / "participants").glob("*.json"):
            path.unlink()
        result = acceptance.validate_evidence(self.root)
        report = acceptance.render_report(result, self.root, "2026-08-27")
        self.assertIn("**BLOCKED**", report)
        self.assertIn("尚无本人真实试用反馈；不生成替代性评价。", report)

    def test_extra_external_participant_cannot_change_owner_scope(self) -> None:
        self.write_json("participants/EM-02.json", participant(2))
        result = acceptance.validate_evidence(self.root)
        self.assertTrue(any("unexpected participant files: EM-02.json" in error for error in result.errors))

    def test_legacy_or_undecided_scope_is_rejected(self) -> None:
        for field, value in (("schema_version", "estate-muse-participant.v1"),
                             ("trial_scope", "external_authors"),
                             ("scope_decided_on", "2026-09-08")):
            with self.subTest(field=field):
                data = participant(1)
                data[field] = value
                self.write_json("participants/EM-01.json", data)
                result = acceptance.validate_evidence(self.root)
                self.assertTrue(any(f".{field}: must be" in error for error in result.errors))

    def test_owner_still_needs_both_action_types(self) -> None:
        data = participant(1)
        data["sessions"][1]["actions"] = []
        self.write_json("participants/EM-01.json", data)
        result = acceptance.validate_evidence(self.root)
        self.assertTrue(any("no successful generate_video" in error for error in result.errors))

    def test_owner_still_needs_three_sessions_and_500_rows(self) -> None:
        for change, expected in (("sessions", "at least 3 real-use sessions"),
                                 ("rows", "row_count: must be at least 500")):
            with self.subTest(change=change):
                data = participant(1)
                if change == "sessions":
                    data["sessions"].pop()
                else:
                    data["sessions"][0]["xlsx"]["row_count"] = 499
                self.write_json("participants/EM-01.json", data)
                result = acceptance.validate_evidence(self.root)
                self.assertTrue(any(expected in error for error in result.errors))

    def test_unfilled_template_is_not_real_evidence(self) -> None:
        template = json.loads((MODULE_PATH.parent / "evidence/templates/participant.template.json").read_text())
        self.write_json("participants/EM-01.json", template)
        result = acceptance.validate_evidence(self.root)
        self.assertFalse(result.passed)
        self.assertTrue(any("owner_confirmed: must be true" in error for error in result.errors))

    def test_malformed_dates_block_without_breaking_progress_report(self) -> None:
        data = participant(1)
        data["trial_started_on"] = "not-a-date"
        self.write_json("participants/EM-01.json", data)
        result = acceptance.validate_evidence(self.root)
        report = acceptance.render_report(result, self.root, "2026-08-27")
        self.assertFalse(result.passed)
        self.assertIn("**BLOCKED**", report)
        self.assertIn("| EM-01 | 公众号作者 | 0 |", report)

    def write_owner_acceptance(self) -> None:
        data = json.loads((MODULE_PATH.parent / "evidence/owner-acceptance.json").read_text())
        self.write_json("owner-acceptance.json", data)

    def test_explicit_owner_acceptance_replaces_missing_records_without_inventing_metrics(self) -> None:
        (self.root / "participants/EM-01.json").unlink()
        self.write_owner_acceptance()
        result = acceptance.validate_evidence(self.root)
        self.assertTrue(result.passed, result.errors)
        self.assertEqual({}, result.participants)
        self.assertEqual(3, len(result.superseded_requirements))
        report = acceptance.render_report(result, self.root, "2026-09-12")
        self.assertIn("本人最终验收：通过（APP-1700）", report)
        self.assertIn("首次试用日期：未知", report)
        self.assertIn("14 天、3 次、结构化会话指标及质量抽样数未核实", report)
        self.assertIn("| EM-01 | — | 未知 | 未知 | 未知 |", report)
        self.assertIn("missing real-participant evidence", report)
        self.assertNotIn("尚无本人真实试用反馈", report)

    def test_owner_acceptance_cannot_waive_technical_or_review_gates(self) -> None:
        (self.root / "participants/EM-01.json").unlink()
        self.write_owner_acceptance()
        technical = json.loads((self.root / "technical-baseline.json").read_text())
        technical["working_tree"]["clean"] = False
        technical["production_trial_path"]["passed"] = False
        self.write_json("technical-baseline.json", technical)
        register = json.loads((self.root / "issue-register.json").read_text())
        register["review_completed"] = False
        register["issues"][0]["status"] = "tracked"
        self.write_json("issue-register.json", register)
        result = acceptance.validate_evidence(self.root)
        self.assertIsNotNone(result.owner_acceptance)
        self.assertEqual(4, len(result.errors), result.errors)
        self.assertTrue(any("working_tree.clean" in error for error in result.errors))
        self.assertTrue(any("production_trial_path.passed" in error for error in result.errors))
        self.assertTrue(any("review_completed" in error for error in result.errors))
        self.assertTrue(any("P1 must be fixed_and_retested" in error for error in result.errors))

    def test_invalid_or_private_owner_acceptance_fails_closed(self) -> None:
        original = json.loads((MODULE_PATH.parent / "evidence/owner-acceptance.json").read_text())
        (self.root / "participants/EM-01.json").unlink()
        for key, value in (("technical_gates_waived", True), ("technical_gates_waived", 0),
                           ("decision", "pending"), ("source", "ai_test"),
                           ("trial_start_date", "2026-08-01"), ("unverified_requirements", []),
                           ("recorded_on", "unknown"), ("email", "person@example.com")):
            with self.subTest(key=key, value=value):
                data = copy.deepcopy(original)
                data[key] = value
                self.write_json("owner-acceptance.json", data)
                result = acceptance.validate_evidence(self.root)
                self.assertFalse(result.passed)
                self.assertIsNone(result.owner_acceptance)
                self.assertTrue(any("missing real-participant evidence" in error for error in result.errors))

    def test_owner_acceptance_never_hides_invalid_existing_evidence(self) -> None:
        self.write_owner_acceptance()
        data = participant(1)
        data["sessions"][0]["actions"][0]["duration_ms"] = 60001
        data["email"] = "person@example.com"
        self.write_json("participants/EM-01.json", data)
        result = acceptance.validate_evidence(self.root)
        self.assertFalse(result.passed)
        self.assertTrue(any("60001 exceeds" in error for error in result.errors))
        self.assertTrue(any("possible email address" in error for error in result.errors))

    def test_dirty_technical_snapshot_is_a_hard_blocker(self) -> None:
        technical = json.loads((self.root / "technical-baseline.json").read_text(encoding="utf-8"))
        technical["working_tree"]["clean"] = False
        self.write_json("technical-baseline.json", technical)
        result = acceptance.validate_evidence(self.root)
        self.assertTrue(any("working_tree.clean: must be true" in error for error in result.errors))


if __name__ == "__main__":
    unittest.main()
