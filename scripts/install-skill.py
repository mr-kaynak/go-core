#!/usr/bin/env python3
"""Install the self-contained core-platform skill for Codex and/or Claude Code."""
import argparse
import base64
import hashlib
import json
import os
import subprocess
from pathlib import Path, PurePosixPath
import re
import shutil
import tempfile
import time
from urllib.error import HTTPError, URLError
from urllib.parse import quote
from urllib.request import Request, urlopen


REPOSITORY = "mr-kaynak/go-core"


def github_json(endpoint):
    headers = {"Accept": "application/vnd.github+json", "User-Agent": "core-platform-skill-installer"}
    token = os.environ.get("GH_TOKEN") or os.environ.get("GITHUB_TOKEN")
    if token:
        headers["Authorization"] = f"Bearer {token}"
    request = Request(f"https://api.github.com/repos/{REPOSITORY}/{endpoint}", headers=headers)
    try:
        with urlopen(request, timeout=30) as response:
            return json.load(response)
    except HTTPError as error:
        raise SystemExit(f"GitHub returned HTTP {error.code}. Check --ref and access; GH_TOKEN can authenticate private or rate-limited requests.") from None
    except URLError as error:
        raise SystemExit(f"Cannot reach GitHub: {error.reason}") from None


def download_skill(ref, destination):
    """Resolve once, then download only the skill subtree at that commit."""
    commit = github_json(f"commits/{quote(ref, safe='')}")
    revision = commit["sha"]
    if not re.fullmatch(r"[0-9a-f]{40}", revision):
        raise SystemExit("GitHub returned an invalid commit ID")
    tree_id = commit["commit"]["tree"]["sha"]
    # Navigate trees before recursive listing: never download the full repo.
    for directory in ("skills", "core-platform"):
        tree = github_json(f"git/trees/{tree_id}")
        entry = next((item for item in tree["tree"] if item["path"] == directory and item["type"] == "tree"), None)
        if entry is None:
            raise SystemExit(f"Skill directory {directory!r} does not exist at {revision}")
        tree_id = entry["sha"]
    tree = github_json(f"git/trees/{tree_id}?recursive=1")
    if tree.get("truncated"):
        raise SystemExit("GitHub returned an incomplete skill tree; nothing installed")
    count = 0
    for item in tree["tree"]:
        path = PurePosixPath(item["path"])
        if path.is_absolute() or ".." in path.parts or "\\" in item["path"]:
            raise SystemExit("Unsafe path in skill tree")
        if item["type"] == "tree":
            continue
        if item["type"] != "blob" or item["mode"] not in ("100644", "100755"):
            raise SystemExit(f"Unsupported skill entry: {path}")
        blob = github_json(f"git/blobs/{item['sha']}")
        if blob.get("encoding") != "base64":
            raise SystemExit(f"Unsupported GitHub encoding: {path}")
        data = base64.b64decode(blob["content"])
        actual = hashlib.sha1(b"blob " + str(len(data)).encode() + b"\0" + data).hexdigest()
        if actual != item["sha"]:
            raise SystemExit(f"Git blob integrity check failed: {path}")
        target = destination.joinpath(*path.parts)
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(data)
        target.chmod(0o755 if item["mode"] == "100755" else 0o644)
        count += 1
    if not (destination / "SKILL.md").is_file():
        raise SystemExit("Downloaded tree has no SKILL.md; nothing installed")
    print(f"Downloaded {count} skill files from {REPOSITORY}@{revision}")
    return revision


def install(source, parent, update, revision):
    target = parent / "core-platform"
    if (target.exists() or target.is_symlink()) and not update:
        raise SystemExit(f"Already installed: {target}. Use --update to back up and replace it.")
    parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix=".core-platform-install-", dir=parent) as temp:
        staged = Path(temp) / "core-platform"
        shutil.copytree(source, staged)
        digest = hashlib.sha256()
        for file in sorted(source.rglob("*")):
            if file.is_file():
                digest.update(str(file.relative_to(source)).encode())
                digest.update(file.read_bytes())
        (staged / "installation.json").write_text(json.dumps({
            "repository": "https://github.com/mr-kaynak/go-core",
            "source_revision": revision,
            "skill_sha256": digest.hexdigest(),
        }, indent=2) + "\n")
        if target.exists() or target.is_symlink():
            # Keep backups outside the skill discovery directory.
            backups = parent.parent / "skill-backups"
            backups.mkdir(exist_ok=True)
            backup = backups / f"core-platform-{time.time_ns()}"
            target.rename(backup)
            print(f"Previous installation: {backup}")
        staged.rename(target)
    print(f"Installed: {target}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--agent", choices=["codex", "claude", "both"], default="both")
    parser.add_argument("--scope", choices=["user", "project"], default="user")
    parser.add_argument("--project-root", type=Path, default=Path.cwd())
    parser.add_argument("--home", type=Path, default=None, help="Override home for isolated install verification (ignores CODEX_HOME)")
    parser.add_argument("--update", action="store_true", help="Back up an existing installation before replacing it")
    parser.add_argument("--ref", help="Download only the skill at this GitHub commit/tag/branch, without cloning (default main when no local source exists)")
    args = parser.parse_args()
    source = Path(__file__).resolve().parent.parent / "skills" / "core-platform"
    base = (args.home or Path.home()) if args.scope == "user" else args.project_root
    agents = ["codex", "claude"] if args.agent == "both" else [args.agent]
    codex_folder = ".agents" if args.scope == "project" else ".codex"
    destinations = [base / (codex_folder if agent == "codex" else ".claude") / "skills" for agent in agents]
    if args.scope == "user" and args.home is None and os.environ.get("CODEX_HOME"):
        destinations = [Path(os.environ["CODEX_HOME"]).expanduser() / "skills" if agent == "codex" else parent
                        for agent, parent in zip(agents, destinations)]
    # Validate all destinations before writing either one.
    for parent in destinations:
        if ((parent / "core-platform").exists() or (parent / "core-platform").is_symlink()) and not args.update:
            parser.error(f"Already installed: {parent / 'core-platform'}; use --update")
    if args.ref or not (source / "SKILL.md").is_file():
        with tempfile.TemporaryDirectory(prefix="core-platform-download-") as temp:
            source = Path(temp) / "core-platform"
            revision = download_skill(args.ref or "main", source)
            for parent in destinations:
                install(source, parent, args.update, revision)
    else:
        try:
            revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=source, text=True, stderr=subprocess.DEVNULL).strip()
            dirty = subprocess.check_output(["git", "status", "--porcelain", "--", str(source)], cwd=source, text=True)
            if dirty:
                revision += "+working-tree"
        except (OSError, subprocess.CalledProcessError):
            revision = "unavailable (archive installation; use skill_sha256)"
        for parent in destinations:
            install(source, parent, args.update, revision)
    print("Start a new agent session. Codex: $core-platform; Claude Code: /core-platform.")


if __name__ == "__main__":
    main()
