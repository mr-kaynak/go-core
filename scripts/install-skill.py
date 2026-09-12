#!/usr/bin/env python3
"""Install the self-contained core-platform skill for Codex and/or Claude Code."""
import argparse
import hashlib
import json
import os
import subprocess
from pathlib import Path
import shutil
import tempfile
import time


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
    args = parser.parse_args()
    source = Path(__file__).resolve().parent.parent / "skills" / "core-platform"
    if not (source / "SKILL.md").is_file():
        parser.error(f"Skill source missing: {source}")
    base = (args.home or Path.home()) if args.scope == "user" else args.project_root
    try:
        revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=source, text=True, stderr=subprocess.DEVNULL).strip()
        dirty = subprocess.check_output(["git", "status", "--porcelain", "--", str(source)], cwd=source, text=True)
        if dirty:
            revision += "+working-tree"
    except (OSError, subprocess.CalledProcessError):
        revision = "unavailable (archive installation; use skill_sha256)"
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
    for parent in destinations:
        install(source, parent, args.update, revision)
    print("Start a new agent session. Codex: $core-platform; Claude Code: /core-platform.")


if __name__ == "__main__":
    main()
