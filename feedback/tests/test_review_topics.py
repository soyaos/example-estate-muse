import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
import xml.etree.ElementTree as ET
import zipfile

SCRIPT = Path(__file__).resolve().parents[1] / "review_topics.py"
SPEC = importlib.util.spec_from_file_location("review_topics", SCRIPT)
review_topics = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(review_topics)


class ReviewTopicsTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.path = Path(self.tmp.name) / "topics.xlsx"
        self.rows = [
            ["如何核实房屋朝向？", "buy", "操作", "拍摄前需核实：拍摄授权与朝向", "low", "短视频"],
            ["如何了解交房时间？", "sell", "对比", "拍摄前需核实：交房约定", "med", "图文+短视频"],
        ]

    def write(self, rows=None, shared=False):
        root = ET.Element("worksheet", xmlns=review_topics.NS["x"])
        data = ET.SubElement(root, "sheetData")
        strings = []
        for number, values in enumerate([review_topics.HEADERS] + (self.rows if rows is None else rows), 1):
            row = ET.SubElement(data, "row", r=str(number))
            for column, value in enumerate(values):
                if value is None:
                    continue
                cell = ET.SubElement(row, "c", r=f"{chr(65 + column)}{number}", t="s" if shared else "inlineStr")
                if shared:
                    ET.SubElement(cell, "v").text = str(len(strings))
                    strings.append(value)
                else:
                    ET.SubElement(ET.SubElement(cell, "is"), "t").text = value
        with zipfile.ZipFile(self.path, "w") as archive:
            archive.writestr("xl/worksheets/sheet1.xml", ET.tostring(root))
            if shared:
                table = ET.Element("sst", xmlns=review_topics.NS["x"])
                for value in strings:
                    ET.SubElement(ET.SubElement(table, "si"), "t").text = value
                archive.writestr("xl/sharedStrings.xml", ET.tostring(table))

    def test_inline_and_shared_valid(self):
        for shared in (False, True):
            with self.subTest(shared=shared):
                self.write(shared=shared)
                result = review_topics.review(self.path, expected_rows=2)
                self.assertTrue(result["passed"])
                self.assertTrue(result["semantic_review_required"])
                self.assertEqual(result["unique_normalized_titles"], 2)

    def test_normalized_duplicate(self):
        self.rows[0][0] = "ＡＢＣ？"
        self.rows[1][0] = " abc? "
        self.write()
        result = review_topics.review(self.path, 2)
        self.assertFalse(result["passed"])
        self.assertIn("duplicate_normalized_title", [x["reason"] for x in result["errors"]])

    def test_sparse_cell_and_blank_row_do_not_pad(self):
        self.rows[0][1] = None
        self.write(self.rows + [["", "", "", "", "", ""]])
        result = review_topics.review(self.path, 2)
        self.assertEqual(result["valid_rows"], 1)
        self.assertEqual(result["errors"][0]["row"], "row-1")

    def test_enums_and_editorial_contract(self):
        self.rows[0] = ["私密错误标题", "unknown", "风险", "私密钩子", "easy", "视频"]
        self.write()
        result = review_topics.review(self.path, 2)
        reasons = [x["reason"] for x in result["errors"]]
        self.assertEqual(reasons.count("invalid_enum"), 4)
        self.assertIn("title_must_end_with_question_mark", reasons)
        self.assertIn("hook_missing_verification_prefix", reasons)
        self.assertNotIn("私密", json.dumps(result, ensure_ascii=False))

    def test_risky_and_negated_phrases_need_review_not_fact_verdict(self):
        for phrase in review_topics.RISK_PHRASES:
            self.rows[0][3] = "拍摄前需核实：禁止" + phrase
            self.write()
            result = review_topics.review(self.path, 2)
            self.assertFalse(result["passed"])
            self.assertEqual(result["errors"], [])
            self.assertEqual(result["review_flags"][0]["row"], "row-1")
            self.assertEqual(result["review_flags"][0]["reason"], "risk_phrase_requires_context_review")

    def test_invalid_file_sanitized(self):
        result = review_topics.review(self.path, 2)
        self.assertFalse(result["passed"])
        self.assertNotIn(str(self.path), json.dumps(result))

    def test_cli_requires_500(self):
        self.write()
        proc = subprocess.run([sys.executable, str(SCRIPT), str(self.path)], capture_output=True, text=True)
        self.assertEqual(proc.returncode, 1)
        self.assertEqual(json.loads(proc.stdout)["expected_rows"], 500)

    def test_500_requires_each_dimension_and_accepts_balanced_rows(self):
        dimensions = sorted(review_topics.ENUMS[1])
        rows = [[f"选题{i}？", dimensions[i % 8], "操作", "拍摄前需核实：资料", "low", "短视频"] for i in range(500)]
        self.write(rows)
        self.assertTrue(review_topics.review(self.path)["passed"])
        for row in rows:
            row[1] = "buy"
        self.write(rows)
        result = review_topics.review(self.path)
        self.assertFalse(result["passed"])
        self.assertEqual(sum(x["reason"] == "insufficient_dimension_coverage" for x in result["errors"]), 7)


if __name__ == "__main__":
    unittest.main()
