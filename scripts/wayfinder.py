#!/usr/bin/env python3
import json, pathlib, sys
root = pathlib.Path(__file__).resolve().parents[1]
issues = {}
for path in sorted((root / "docs/wayfinder/issues").glob("*.md")):
    line = path.read_text().splitlines()[0]
    item = json.loads(line.removeprefix("<!-- ").removesuffix(" -->"))
    if item["id"] in issues:
        raise SystemExit("duplicate issue identity")
    item["path"] = str(path.relative_to(root))
    issues[item["id"]] = item
for item in issues.values():
    for ref in item["blocked_by"] + ([item["parent"]] if item["parent"] else []):
        if ref not in issues:
            raise SystemExit(f"unknown issue: {ref}")
    if item["status"] not in ("open", "closed"):
        raise SystemExit("invalid issue status")
visiting, visited = set(), set()
def visit(key):
    if key in visiting:
        raise SystemExit("dependency cycle")
    if key in visited:
        return
    visiting.add(key)
    for dep in issues[key]["blocked_by"]:
        visit(dep)
    visiting.remove(key)
    visited.add(key)
for key in issues:
    visit(key)
command = sys.argv[1] if len(sys.argv) > 1 else "frontier"
if command == "check":
    print(f"tracker: {len(issues)} valid issues")
elif command in ("frontier", "list"):
    for item in issues.values():
        if not item["parent"]:
            continue
        blocked = any(issues[ref]["status"] != "closed" for ref in item["blocked_by"])
        if command == "frontier" and (item["status"] != "open" or item["assignee"] or blocked):
            continue
        print(json.dumps(item, sort_keys=True))
else:
    raise SystemExit("usage: wayfinder.py [frontier|list|check]")
