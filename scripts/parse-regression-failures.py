#!/usr/bin/env python3
"""Parse encore test output for TestConversationRegressionAutoGen path failures."""
import json
import re
import sys

CASE_RE = re.compile(r"--- FAIL: TestConversationRegressionAutoGen/(\S+)")
PATH_RE = re.compile(r'path = "([^"]*)" want "([^"]*)" reply="(.*)"\s*$')


def main() -> int:
    if len(sys.argv) < 2:
        print("[]")
        return 0
    path = sys.argv[1]
    try:
        with open(path, encoding="utf-8", errors="replace") as f:
            lines = f.readlines()
    except OSError:
        print("[]")
        return 0

    failures = []
    current_case = ""
    for line in lines:
        m_case = CASE_RE.search(line)
        if m_case:
            current_case = m_case.group(1)
            continue
        m_path = PATH_RE.search(line.strip())
        if m_path and current_case:
            reply = m_path.group(3)
            if len(reply) > 200:
                reply = reply[:200] + "…"
            failures.append(
                {
                    "caseName": current_case,
                    "gotPath": m_path.group(1),
                    "wantPath": m_path.group(2),
                    "replyPreview": reply,
                }
            )
            current_case = ""

    json.dump(failures, sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
