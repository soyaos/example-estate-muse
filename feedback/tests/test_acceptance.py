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
        "participant_id": participant_id,
        "profile": "公众号作者",
        "trial_started_on": "2026-09-01",
        "trial_ended_on": "2026-09-14",
        "attestation": {
            "real_human": True,
            "real_estate_content_creator": True,
            "fourteen_day_trial_consented": True,
            "anonymous_feedback_confirmed": True,
            "coordinator_verified": True,
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
        for index in range(1, 6):
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
        self.assertIn("真人参与者：5 / 5", report)
        self.assertNotIn("sk-", report)

    def test_missing_participant_is_a_hard_blocker(self) -> None:
        (self.root / "participants" / "EM-05.json").unlink()
        result = acceptance.validate_evidence(self.root)
        self.assertFalse(result.passed)
        self.assertTrue(any("EM-05.json: missing" in error for error in result.errors))

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

    def test_open_p1_or_untracked_p2_is_rejected(self) -> None:
        register = {
            "schema_version": acceptance.ISSUE_SCHEMA,
            "review_completed": True,
            "issues": [
                {"code": "EM-P1-001", "severity": "P1", "status": "tracked", "linear_issue": "APP-2001"},
                {"code": "EM-P2-001", "severity": "P2", "status": "fixed_and_retested", "linear_issue": "APP-2002"},
            ],
        }
        self.write_json("issue-register.json", register)
        result = acceptance.validate_evidence(self.root)
        self.assertTrue(any("P1 must be fixed_and_retested" in error for error in result.errors))
        self.assertTrue(any("P2 must be tracked" in error for error in result.errors))

    def test_report_preserves_blocked_truth_without_inventing_feedback(self) -> None:
        for path in (self.root / "participants").glob("*.json"):
            path.unlink()
        result = acceptance.validate_evidence(self.root)
        report = acceptance.render_report(result, self.root, "2026-08-27")
        self.assertIn("**BLOCKED**", report)
        self.assertIn("尚无真实作者反馈；不生成替代性评价。", report)

    def test_malformed_dates_block_without_breaking_progress_report(self) -> None:
        data = participant(1)
        data["trial_started_on"] = "not-a-date"
        self.write_json("participants/EM-01.json", data)
        result = acceptance.validate_evidence(self.root)
        report = acceptance.render_report(result, self.root, "2026-08-27")
        self.assertFalse(result.passed)
        self.assertIn("**BLOCKED**", report)
        self.assertIn("| EM-01 | 公众号作者 | 0 |", report)

    def test_dirty_technical_snapshot_is_a_hard_blocker(self) -> None:
        technical = json.loads((self.root / "technical-baseline.json").read_text(encoding="utf-8"))
        technical["working_tree"]["clean"] = False
        self.write_json("technical-baseline.json", technical)
        result = acceptance.validate_evidence(self.root)
        self.assertTrue(any("working_tree.clean: must be true" in error for error in result.errors))


if __name__ == "__main__":
    unittest.main()
