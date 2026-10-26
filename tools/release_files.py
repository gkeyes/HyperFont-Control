#!/usr/bin/env python3
"""Write update.json and changelog.md for a GitHub release next to the built ZIP.

The module's updateJson points at releases/latest/download/update.json, so the
root manager offers whatever the newest published (non-prerelease) release holds.
"""
import json
import os
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parents[1]
tag = sys.argv[1] if len(sys.argv) > 1 else ""
prop = dict(line.split("=", 1) for line in (ROOT / "module/module.prop").read_text().splitlines() if "=" in line)
version = prop["version"]
if tag != "v" + version:
    raise SystemExit(f"Release tag {tag!r} must be v{version} (module.prop version)")
dist = Path(os.environ.get("HFC_DIST", str(ROOT / "dist")))
archive = dist / f"HyperFont-Control-v{version}.zip"
if not archive.is_file():
    raise SystemExit(f"{archive} not built")

notes, section = [], False
for line in (ROOT / "CHANGELOG.md").read_text(encoding="utf-8").splitlines():
    if line.startswith("## "):
        if section: break
        section = line[3:].strip() == version
    if section: notes.append(line)
if not notes:
    raise SystemExit(f"CHANGELOG.md has no '## {version}' section")
(dist / "changelog.md").write_text("\n".join(notes).strip() + "\n", encoding="utf-8")

base = f"https://github.com/{os.environ.get('GITHUB_REPOSITORY', 'gkeyes/HyperFont-Control')}/releases/download/{tag}"
update = {"version": version, "versionCode": int(prop["versionCode"]), "zipUrl": f"{base}/{archive.name}", "changelog": f"{base}/changelog.md"}
(dist / "update.json").write_text(json.dumps(update, indent=2) + "\n", encoding="utf-8")
print(json.dumps(update))
