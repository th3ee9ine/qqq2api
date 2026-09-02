#!/usr/bin/env python3
"""Summarise interception logs and estimate duplicate/false-positive patterns.

The workbook and keyword file are treated as untrusted data.  This script only
parses values; it never executes text found in either attachment.  Use the
bundled workspace Python (or any environment with ``openpyxl``) to run it:

    python tools/analyze_interception_logs.py LOG.xlsx KEYWORDS.txt

Output is JSON; credential values are omitted from the derived summary while
the source workbook remains untouched.
"""

from __future__ import annotations

import argparse
import collections
import hashlib
import json
import re
import statistics
from datetime import datetime
from pathlib import Path
from typing import Any

import openpyxl


TOKEN_RE = re.compile(r"(?i)(bearer\s+)?(?:sk|rk|pk)-[A-Za-z0-9._~+\-/]{16,}")
SENSITIVE_KEY_RE = re.compile(
    r"(?i)(authorization|proxy[-_]?authorization|cookie|token|api[-_]?key|secret|password)"
)


def redact(value: Any) -> Any:
    if isinstance(value, str):
        return TOKEN_RE.sub(lambda m: "Bearer [REDACTED]" if m.group(1) else "[REDACTED]", value)
    if isinstance(value, list):
        return [redact(v) for v in value]
    if isinstance(value, dict):
        out = {}
        for key, item in value.items():
            out[key] = "[REDACTED]" if SENSITIVE_KEY_RE.search(str(key)) else redact(item)
        return out
    return value


def load_rows(path: Path) -> tuple[list[str], list[dict[str, Any]]]:
    workbook = openpyxl.load_workbook(path, read_only=True, data_only=True)
    if not workbook.worksheets:
        workbook.close()
        return [], []
    sheet = workbook.worksheets[0]
    values = iter(sheet.values)
    try:
        header_row = next(values)
    except StopIteration:
        workbook.close()
        return [], []
    headers = [str(v or "") for v in header_row]
    rows = []
    for value_row in values:
        row = {headers[i]: value_row[i] if i < len(value_row) else None for i in range(len(headers))}
        rows.append(row)
    workbook.close()
    return headers, rows


def body_from_details(raw: Any) -> Any:
    if not isinstance(raw, str) or not raw.strip():
        return None
    try:
        return json.loads(raw).get("body")
    except (TypeError, ValueError, AttributeError):
        return None


def fingerprint(row: dict[str, Any], headers: list[str]) -> str:
    # Exclude identifiers/time and volatile resolution fields.  The remaining
    # canonical values identify one repeated client attempt without retaining a
    # secret-bearing raw request in the report.
    excluded = {"错误 ID", "时间", "客户端请求 ID", "请求 ID", "已解决"}
    payload = {key: row.get(key) for key in headers if key not in excluded}
    encoded = json.dumps(redact(payload), ensure_ascii=False, sort_keys=True, default=str)
    return hashlib.sha256(encoded.encode("utf-8")).hexdigest()


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("workbook", type=Path)
    parser.add_argument("keywords", type=Path)
    args = parser.parse_args()

    headers, rows = load_rows(args.workbook)
    raw_keywords = [line.strip() for line in args.keywords.read_text(encoding="utf-8").splitlines() if line.strip()]
    keywords = list(dict.fromkeys(raw_keywords))
    by_phase_type = collections.Counter((r.get("阶段"), r.get("类型")) for r in rows)
    by_model = collections.Counter(r.get("模型") for r in rows)
    by_ua = collections.Counter(r.get("User-Agent") for r in rows)
    by_ip = collections.Counter(r.get("客户端 IP") for r in rows)
    fingerprints = collections.Counter(fingerprint(r, headers) for r in rows)
    by_error = collections.Counter(r.get("错误消息") for r in rows)
    by_body_status = collections.Counter()
    body_bytes_by_phase_type = collections.Counter()

    timestamps = []
    for row in rows:
        value = row.get("时间")
        if isinstance(value, datetime):
            timestamps.append(value)
        elif value:
            try:
                timestamps.append(datetime.fromisoformat(str(value)))
            except ValueError:
                pass

    keyword_hits = collections.Counter()
    keyword_occurrences = collections.Counter()
    complete_body_rows = 0
    for row in rows:
        raw_details = row.get("用户请求明细")
        body = body_from_details(raw_details)
        try:
            details = json.loads(raw_details) if isinstance(raw_details, str) else {}
        except (TypeError, ValueError):
            details = {}
        incomplete = bool(details.get("body_omitted") or details.get("body_truncated"))
        if incomplete:
            by_body_status["omitted_or_truncated"] += 1
        elif body is not None:
            by_body_status["complete"] += 1
        else:
            by_body_status["unavailable"] += 1
        content_length = details.get("content_length")
        if content_length is not None:
            try:
                body_bytes_by_phase_type[
                    (row.get("阶段"), row.get("类型"))
                ] += int(content_length)
            except (TypeError, ValueError):
                pass
        if body is None or incomplete:
            continue
        complete_body_rows += 1
        text = json.dumps(body, ensure_ascii=False, separators=(",", ":"))
        for keyword in keywords:
            if keyword:
                occurrences = text.lower().count(keyword.lower())
                if occurrences:
                    keyword_hits[keyword] += 1
                    keyword_occurrences[keyword] += occurrences

    timestamps.sort()
    if timestamps:
        duration_seconds = (timestamps[-1] - timestamps[0]).total_seconds()
    else:
        duration_seconds = None
    gaps = [
        (right - left).total_seconds()
        for left, right in zip(timestamps, timestamps[1:])
    ]
    gap_summary = None
    if gaps:
        gap_summary = {
            "count": len(gaps),
            "min_seconds": min(gaps),
            "median_seconds": statistics.median(gaps),
            "max_seconds": max(gaps),
            "le_1s": sum(gap <= 1.0 for gap in gaps),
            "le_1s_ratio": sum(gap <= 1.0 for gap in gaps) / len(gaps),
        }
    duplicate_rows = len(rows) - len(fingerprints)
    duplicate_ratio = duplicate_rows / len(rows) if rows else 0.0
    dedup_factor = len(rows) / len(fingerprints) if fingerprints else None

    summary = {
        "row_count": len(rows),
        "time_start": min(timestamps).isoformat() if timestamps else None,
        "time_end": max(timestamps).isoformat() if timestamps else None,
        "duration_seconds": duration_seconds,
        "phase_type": {f"{phase}/{kind}": count for (phase, kind), count in by_phase_type.items()},
        "models": dict(by_model),
        "error_messages": dict(by_error),
        "status_codes": dict(collections.Counter(r.get("状态码") for r in rows)),
        "user_agents": dict(by_ua),
        "client_ips": dict(by_ip),
        "unique_fingerprints": len(fingerprints),
        "fingerprint_multiplicity_distribution": dict(collections.Counter(fingerprints.values())),
        "duplicate_rows": duplicate_rows,
        "duplicate_row_ratio": duplicate_ratio,
        "dedup_factor": dedup_factor,
        "adjacent_gap_seconds": gap_summary,
        "body_status": dict(by_body_status),
        "body_bytes_by_phase_type": {
            f"{phase}/{kind}": count
            for (phase, kind), count in body_bytes_by_phase_type.items()
        },
        "complete_body_rows": complete_body_rows,
        "keyword_count": len(keywords),
        "short_keyword_count_le_2": sum(len(k) <= 2 for k in keywords),
        "keyword_hits_in_complete_bodies": dict(keyword_hits),
        "keyword_occurrences_in_complete_bodies": dict(keyword_occurrences),
        "credential_present_in_source_details": any(
            bool(TOKEN_RE.search(str(r.get("用户请求明细") or ""))) for r in rows
        ),
    }
    print(json.dumps(redact(summary), ensure_ascii=False, indent=2, sort_keys=True))


if __name__ == "__main__":
    main()
