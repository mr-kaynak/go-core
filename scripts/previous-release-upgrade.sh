#!/usr/bin/env bash
#
# Verify that a database built by the previous release's migrations upgrades
# cleanly to this commit's, with its data intact.
#
# This is the only check that exercises the path every real deployment takes.
# The suite that runs on every PR builds databases from scratch, which is the
# one situation upgrades are never in.
#
# Until the first release exists there is no predecessor to upgrade from, so
# the check reports that it did nothing and exits successfully. It must not
# report success quietly: a missing tag and a passing upgrade look identical
# in a green checkmark, and this script is here precisely to make the
# difference visible.

set -euo pipefail

readonly DSN="${GOCORE_TEST_POSTGRES_DSN:?GOCORE_TEST_POSTGRES_DSN must be set}"

note() { printf '::notice::%s\n' "$*"; }
fail() { printf '::error::%s\n' "$*" >&2; exit 1; }

# A shallow clone silently hides every tag, which would turn this check into a
# permanent no-op without anyone noticing.
if [ "$(git rev-parse --is-shallow-repository)" = "true" ]; then
  fail "the repository is a shallow clone, so release tags are not visible.
Set 'fetch-depth: 0' on the checkout step; a truncated history must fail here
rather than silently skip the upgrade check."
fi

# Eligible predecessors are stable releases only. Pre-releases are candidates
# that were never promoted, so upgrading from one proves nothing about the
# path real consumers take.
#
# Once release manifests exist (Phase C-2), eligibility tightens further to
# releases that actually completed promotion.
predecessor=""
seen=0
head_sha="$(git rev-parse HEAD)"

while IFS= read -r tag; do
  [ -n "$tag" ] || continue
  seen=$((seen + 1))
  case "$tag" in
    *-*) continue ;;  # pre-release: v1.2.3-rc.1
  esac
  if [ "$(git rev-list -n1 "$tag")" = "$head_sha" ]; then
    continue          # the candidate itself: comparing it with itself proves nothing
  fi
  if [ -z "$predecessor" ]; then
    predecessor="$tag"
  fi
done <<EOF
$(git tag --list 'v*' --sort=-v:refname)
EOF

if [ -z "$predecessor" ]; then
  note "No stable release tag exists yet, so there is no previous release to upgrade from.
This check is inert until the first release and becomes mandatory afterwards.
Tags seen: ${seen}"
  exit 0
fi

note "Upgrading a database built by ${predecessor} to HEAD (${head_sha})"

readonly WORKTREE="$(mktemp -d)"
cleanup() { git worktree remove --force "$WORKTREE" >/dev/null 2>&1 || true; }
trap cleanup EXIT

git worktree add --detach "$WORKTREE" "$predecessor" >/dev/null

# Build the database with the predecessor's migrations, seed it, then run
# HEAD's migrations over the same database and assert the seed survived.
GOCORE_UPGRADE_FROM="$predecessor" \
GOCORE_UPGRADE_WORKTREE="$WORKTREE" \
  go test ./internal/infrastructure/database/... -run TestUpgradeFromPreviousRelease -v

note "Upgrade from ${predecessor} succeeded"
