"""Deterministic XLSX preflight; neither fact checking nor semantic approval."""

import argparse
import collections
import json
import re
import unicodedata
import xml.etree.ElementTree as ET
import zipfile

HEADERS = ["标题", "维度", "切面", "钩子", "难度", "建议产物"]
ENUMS = {
    1: {"buy", "hold", "sell", "market", "policy", "lifestyle", "risk", "compare"},
    2: {"数据", "案例", "对比", "操作", "争议"},
    4: {"low", "med", "high"},
    5: {"图文", "短视频", "图文+短视频"},
}
RISK_PHRASES = ("确保增值", "稳赚", "点燃纸张", "打火机测试", "尾随进入", "大功率电器同时开启")
NS = {"x": "http://schemas.openxmlformats.org/spreadsheetml/2006/main"}


def normalize_title(value):
    return " ".join(unicodedata.normalize("NFKC", value).split()).casefold()


def read_rows(path):
    """Read sheet1 with sparse column positions preserved; never extract ZIP files."""
    with zipfile.ZipFile(path) as archive:
        shared = []
        if "xl/sharedStrings.xml" in archive.namelist():
            root = ET.fromstring(archive.read("xl/sharedStrings.xml"))
            shared = ["".join(t.text or "" for t in item.findall(".//x:t", NS))
                      for item in root.findall("x:si", NS)]
        root = ET.fromstring(archive.read("xl/worksheets/sheet1.xml"))
        rows = []
        for row in root.findall(".//x:sheetData/x:row", NS):
            values = []
            for cell in row.findall("x:c", NS):
                reference = cell.get("r", "")
                match = re.fullmatch(r"([A-Z]+)[1-9][0-9]*", reference)
                if not match:
                    raise ValueError("invalid cell reference")
                column = 0
                for letter in match.group(1):
                    column = column * 26 + ord(letter) - ord("A") + 1
                if column > 16384:
                    raise ValueError("invalid column")
                while len(values) < column:
                    values.append("")
                if cell.find("x:f", NS) is not None:
                    raise ValueError("formula cells are not topic text")
                value = cell.findtext("x:v", default="", namespaces=NS)
                if cell.get("t") == "s":
                    index = int(value)
                    if index < 0 or index >= len(shared):
                        raise ValueError("invalid shared string index")
                    value = shared[index]
                elif cell.get("t") == "inlineStr":
                    value = "".join(t.text or "" for t in cell.findall(".//x:t", NS))
                values[column - 1] = value
            row_number = int(row.get("r", "0"))
            if row_number <= len(rows) or row_number > 1048576:
                raise ValueError("invalid row position")
            rows.extend([[] for _ in range(row_number - len(rows) - 1)])
            rows.append(values)
        return rows


def review(path, expected_rows=500):
    result = {
        "check": "deterministic_preflight_only", "expected_rows": expected_rows,
        "valid_rows": 0, "unique_normalized_titles": 0, "dimensions": {},
        "errors": [], "review_flags": [], "passed": False,
        "semantic_review_required": True,
        "note": "通过不代表事实核验或语义审核通过；风险词含否定语境也仅标记待复核，需 AI 或人工语义复核。",
    }
    try:
        rows = read_rows(path)
    except (OSError, ValueError, KeyError, IndexError, ET.ParseError, zipfile.BadZipFile, RuntimeError):
        result["errors"].append({"row": None, "reason": "xlsx_read_failed"})
        return result
    if not rows or rows[0] != HEADERS:
        result["errors"].append({"row": None, "reason": "expected_six_headers"})
    titles = {}
    dimensions = collections.Counter()
    for index, row in enumerate(rows[1:], 1):
        row_id = f"row-{index}"
        if not any(cell.strip() for cell in row):
            continue
        if len(row) != 6 or not all(cell.strip() for cell in row):
            result["errors"].append({"row": row_id, "reason": "expected_six_nonempty_cells"})
            continue
        result["valid_rows"] += 1
        dimensions[row[1]] += 1
        title = normalize_title(row[0])
        if title in titles:
            result["errors"].append({"row": row_id, "reason": "duplicate_normalized_title", "duplicate_of": titles[title]})
        else:
            titles[title] = row_id
        for column, allowed in ENUMS.items():
            if row[column] not in allowed:
                result["errors"].append({"row": row_id, "reason": "invalid_enum", "column": HEADERS[column]})
        if not row[0].rstrip().endswith(("？", "?")):
            result["errors"].append({"row": row_id, "reason": "title_must_end_with_question_mark"})
        if not row[3].startswith("拍摄前需核实："):
            result["errors"].append({"row": row_id, "reason": "hook_missing_verification_prefix"})
        for phrase in RISK_PHRASES:
            if any(phrase in cell for cell in row):
                result["review_flags"].append({"row": row_id, "reason": "risk_phrase_requires_context_review", "rule": phrase})
    result["unique_normalized_titles"] = len(titles)
    result["dimensions"] = {key: dimensions[key] for key in sorted(ENUMS[1]) if dimensions[key]}
    if expected_rows == 500:
        for dimension in sorted(ENUMS[1]):
            if dimensions[dimension] < 50:
                result["errors"].append({"row": None, "reason": "insufficient_dimension_coverage", "dimension": dimension, "minimum": 50})
    if result["valid_rows"] != expected_rows:
        result["errors"].append({"row": None, "reason": "wrong_valid_row_count"})
    if len(titles) != expected_rows:
        result["errors"].append({"row": None, "reason": "wrong_unique_title_count"})
    result["passed"] = not result["errors"] and not result["review_flags"]
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("xlsx")
    args = parser.parse_args()
    result = review(args.xlsx)
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0 if result["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
