#!/usr/bin/env python3
"""Build, validate and package a font-free module from this source checkout."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import zipfile
from build_compat import build as build_compat

ROOT = Path(__file__).resolve().parents[1]
VERSION = "2.0.0"
CACHE = Path.home() / ".cache/hyperfont-control-build"
CACHE.mkdir(parents=True, exist_ok=True)
JOBS = max(1, min(8, int(os.environ.get("HFC_BUILD_JOBS", "1"))))
env = dict(os.environ, GOMAXPROCS=str(JOBS), CGO_ENABLED="0", GOCACHE=str(CACHE / "go-cache"))
SDK = Path(os.environ.get("ANDROID_SDK_ROOT", os.environ.get("ANDROID_HOME", "")))
JAVA = Path(os.environ["JAVA_HOME"]) if os.environ.get("JAVA_HOME") else Path(shutil.which("javac") or "javac").resolve().parents[1]
PLATFORM = os.environ.get("HFC_ANDROID_PLATFORM", "android-35")
BUILD_TOOLS = os.environ.get("HFC_ANDROID_BUILD_TOOLS", "35.0.0")
if not (SDK / "platforms" / PLATFORM / "android.jar").is_file():
    raise SystemExit(f"Install SDK {PLATFORM} and set ANDROID_SDK_ROOT (or ANDROID_HOME).")
env["JAVA_HOME"] = str(JAVA)
env["JAVA_TOOL_OPTIONS"] = f"-XX:ActiveProcessorCount={JOBS}"
def run(args, **kwargs):
    subprocess.run([str(x) for x in args], cwd=ROOT, env=kwargs.pop("env", env), check=True, **kwargs)

run(["go", "test", f"-p={JOBS}", "./..."])
run(["go", "vet", f"-p={JOBS}", "./..."])
run(["node", "--test", "tests/web_weights.test.mjs"])
with tempfile.TemporaryDirectory(prefix="package-", dir=CACHE) as temporary:
    stage = Path(temporary) / "module"
    shutil.copytree(ROOT / "module", stage)
    for arch, label in [("arm64", "arm64-v8a"), ("x86_64", "x86_64")]:
        build_compat(stage / "zygisk" / (label + ".so"), arch)
    for arch, label in [("arm64", "arm64"), ("amd64", "x86_64")]:
        output = stage / "bin" / label / "hfctl"
        output.parent.mkdir(parents=True, exist_ok=True)
        run(["go", "build", f"-p={JOBS}", "-trimpath", "-buildvcs=false", "-ldflags=-s -w -buildid=", "-o", output, "./cmd/hfctl"], env=dict(env, GOOS="linux", GOARCH=arch))
        shutil.copy2(output, CACHE / f"hfctl-{label}")
    run(["go", "build", f"-p={JOBS}", "-trimpath", "-o", CACHE / "hfdev", "./cmd/hfdev"])
    classes = Path(temporary) / "classes"
    dex = Path(temporary) / "dex"
    classes.mkdir(); dex.mkdir()
    android = SDK / "platforms" / PLATFORM / "android.jar"
    run([JAVA / "bin/javac", "-source", "8", "-target", "8", "-classpath", android, "-d", classes, ROOT / "android/VerifyFonts.java"])
    run([SDK / "build-tools" / BUILD_TOOLS / "d8", "--min-api", "29", "--lib", android, "--output", dex, classes / "local/hyperfont/VerifyFonts.class"])
    with zipfile.ZipFile(stage / "bin/verify-fonts.jar", "w", zipfile.ZIP_DEFLATED) as z:
        item = zipfile.ZipInfo("classes.dex", (2026, 10, 24, 0, 0, 0))
        item.compress_type = zipfile.ZIP_DEFLATED
        item.external_attr = 0o100644 << 16
        z.writestr(item, (dex / "classes.dex").read_bytes())
    shutil.copy2(stage / "bin/verify-fonts.jar", CACHE / "verify-fonts.jar")
    shutil.copy2(ROOT / "LICENSE", stage / "LICENSE")
    revision = subprocess.run(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True, capture_output=True)
    source_commit = revision.stdout.strip() if revision.returncode == 0 else "unversioned"
    (stage / "SOURCE.txt").write_text("HyperFont Control " + VERSION + "\nSource: https://github.com/gkeyes/HyperFont-Control\nCommit: " + source_commit + "\nOriginal font compatibility adapter; no MFGA fonts or binaries bundled.\nThird-party API header sources and licenses: third_party/.\nWebUI bridge protocol: https://kernelsu.org/guide/module-webui.html\n", encoding="utf-8")
    shutil.copytree(ROOT / "native/third_party", stage / "third_party")
    for script in stage.rglob("*.sh"):
        run(["sh", "-n", script])
    run(["sh", "-n", stage / "META-INF/com/google/android/update-binary"])
    run(["node", "--check", stage / "webroot/app.js"])
    dist = Path(os.environ.get("HFC_DIST", str(ROOT / "dist")))
    dist.mkdir(exist_ok=True)
    archive = dist / f"HyperFont-Control-v{VERSION}.zip"
    with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED, compresslevel=9) as z:
        for path in sorted(stage.rglob("*")):
            if not path.is_file(): continue
            relative = path.relative_to(stage).as_posix()
            info = zipfile.ZipInfo(relative, (2026, 10, 24, 0, 0, 0))
            executable = path.suffix == ".sh" or path.name in ("hfctl", "update-binary")
            info.external_attr = (0o100755 if executable else 0o100644) << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            z.writestr(info, path.read_bytes())
    with zipfile.ZipFile(archive) as z:
        assert z.testzip() is None
        names = set(z.namelist())
        assert {"skip_mount", "module.prop", "customize.sh", "post-mount.sh", "service.sh", "boot-completed.sh", "uninstall.sh", "webroot/index.html", "webroot/weights.js", "bin/arm64/hfctl", "bin/x86_64/hfctl", "bin/verify-fonts.jar", "zygisk/arm64-v8a.so", "zygisk/x86_64.so"} <= names
        assert not any(n.startswith("system/") for n in names)
        assert not any(n.endswith((".ttf", ".otf", ".ttc")) for n in names)
        assert not any("libc-import" in n or n.endswith("/libc.so") for n in names)
    sha = hashlib.sha256(archive.read_bytes()).hexdigest()
    archive.with_suffix(".zip.sha256").write_text(f"{sha}  {archive.name}\n")
    result = {"version": VERSION, "sourceCommit": source_commit, "archive": archive.name, "bytes": archive.stat().st_size, "sha256": sha, "tests": "host unit, go vet, Web mapping regression, shell syntax, Java/D8 build, JS syntax, native ELF imports/exports/16K alignment, ZIP CRC", "realFontTestsRequested": bool(env.get("HFC_REAL_FONTS")), "phoneInstallation": False, "runtimeMountValidation": False}
    (dist / "build-report.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps(result, ensure_ascii=False))
