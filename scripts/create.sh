#!/bin/bash
#
# create.sh - Bootstrap a new project from the go-core skeleton WITHOUT cloning first.
#
# This is the zero-clone path: it clones the skeleton into a new directory and
# then delegates all identity rewriting to scripts/init-project.sh.
#
# Usage — interactive (recommended):
#   bash <(curl -fsSL https://raw.githubusercontent.com/mr-kaynak/go-core/main/scripts/create.sh)
#
# Usage — non-interactive:
#   curl -fsSL https://raw.githubusercontent.com/mr-kaynak/go-core/main/scripts/create.sh \
#     | bash -s -- github.com/acme/orders-api "Orders API"
#
#   Positional arguments:
#     $1  Go module path  (e.g. github.com/acme/orders-api)
#     $2  Display name    (optional; default derived from module path)
#     $3  Target dir      (optional; default: ./<last-path-segment>)
#
# Environment overrides:
#   GO_CORE_REPO  Override the clone URL (useful for local testing).
#                 Default: https://github.com/mr-kaynak/go-core.git

set -euo pipefail

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------

GO_CORE_REPO="${GO_CORE_REPO:-https://github.com/mr-kaynak/go-core.git}"

# ---------------------------------------------------------------------------
# Colors  (only when stdout is a terminal)
# ---------------------------------------------------------------------------

if [ -t 1 ]; then
    C_RESET=$'\033[0m'; C_BOLD=$'\033[1m'
    C_RED=$'\033[31m'; C_GREEN=$'\033[32m'; C_YELLOW=$'\033[33m'
    C_BLUE=$'\033[34m'; C_CYAN=$'\033[36m'; C_DIM=$'\033[2m'
else
    C_RESET=''; C_BOLD=''; C_RED=''; C_GREEN=''; C_YELLOW=''
    C_BLUE=''; C_CYAN=''; C_DIM=''
fi

info() { printf '%s\n' "${C_CYAN}==>${C_RESET} $*"; }
step() { printf '%s\n' "  ${C_GREEN}OK${C_RESET} $*"; }
warn() { printf '%s\n' "${C_YELLOW}!${C_RESET}  $*" >&2; }
err()  { printf '%s\n' "${C_RED}ERROR: $*${C_RESET}" >&2; }
die()  { err "$@"; exit 1; }
hdr()  { printf '\n%s\n' "${C_BOLD}${C_BLUE}$*${C_RESET}"; }

# ---------------------------------------------------------------------------
# Preflight checks
# ---------------------------------------------------------------------------

preflight() {
    local missing=0
    if ! command -v git >/dev/null 2>&1; then
        err "git is required but was not found in PATH."
        missing=1
    fi
    if ! command -v go >/dev/null 2>&1; then
        err "go is required but was not found in PATH."
        missing=1
    fi
    if [ "$missing" -eq 1 ]; then
        die "Install the missing tools and try again."
    fi
}

# ---------------------------------------------------------------------------
# Derivation helpers  (mirrors init-project.sh conventions)
# ---------------------------------------------------------------------------

# Last path segment: github.com/acme/orders-api -> orders-api
derive_slug() {
    printf '%s' "${1##*/}"
}

# Title-case from slug: orders-api -> Orders Api
derive_display() {
    printf '%s' "$1" | sed 's/[-_]/ /g' | awk '{
        for (i = 1; i <= NF; i++) {
            $i = toupper(substr($i, 1, 1)) substr($i, 2)
        }
        print
    }'
}

# ---------------------------------------------------------------------------
# Validation
# ---------------------------------------------------------------------------

validate_module() {
    local mod="$1"
    if [ -z "$mod" ]; then
        err "Module path must not be empty."
        return 1
    fi
    if printf '%s' "$mod" | grep -qE '[[:space:]]'; then
        err "Module path must not contain spaces: '$mod'"
        return 1
    fi
    if ! printf '%s' "$mod" | grep -qE '^[a-zA-Z0-9][a-zA-Z0-9._-]*(/[a-zA-Z0-9._-]+)+$'; then
        err "Module path '$mod' does not look like a Go module path (e.g. github.com/acme/orders-api)."
        return 1
    fi
    if [ "$mod" = "github.com/mr-kaynak/go-core" ]; then
        err "That is the skeleton's own module path. Choose a new one."
        return 1
    fi
    return 0
}

# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------

NEW_MODULE=""
NEW_DISPLAY=""
TARGET_DIR=""

parse_args() {
    local pos=0
    while [ $# -gt 0 ]; do
        case "$1" in
            -h|--help)
                printf '%s\n' ""
                printf '%s\n' "${C_BOLD}Create a new project from the go-core skeleton.${C_RESET}"
                printf '%s\n' ""
                printf '%s\n' "${C_BOLD}Usage (interactive):${C_RESET}"
                printf '%s\n' "  bash <(curl -fsSL https://raw.githubusercontent.com/mr-kaynak/go-core/main/scripts/create.sh)"
                printf '%s\n' ""
                printf '%s\n' "${C_BOLD}Usage (non-interactive):${C_RESET}"
                printf '%s\n' "  $0 <module-path> [display-name] [target-dir]"
                printf '%s\n' ""
                printf '%s\n' "${C_BOLD}Arguments:${C_RESET}"
                printf '%s\n' "  module-path   Go module path, e.g. github.com/acme/orders-api"
                printf '%s\n' "  display-name  Human name, e.g. \"Orders API\" (default: derived from module)"
                printf '%s\n' "  target-dir    Directory to create the project in (default: ./<slug>)"
                printf '%s\n' ""
                printf '%s\n' "${C_BOLD}Environment:${C_RESET}"
                printf '%s\n' "  GO_CORE_REPO  Override the clone URL (default: GitHub)"
                printf '%s\n' ""
                exit 0
                ;;
            -*)
                die "Unknown flag: $1 (use --help)"
                ;;
            *)
                pos=$((pos + 1))
                case "$pos" in
                    1) NEW_MODULE="$1" ;;
                    2) NEW_DISPLAY="$1" ;;
                    3) TARGET_DIR="$1" ;;
                    *) die "Too many arguments. See --help." ;;
                esac
                shift
                continue
                ;;
        esac
        shift
    done
}

# ---------------------------------------------------------------------------
# Interactive prompts
# ---------------------------------------------------------------------------

prompt_inputs() {
    hdr "go-core project bootstrap"
    printf '%s\n\n' "${C_DIM}Clone the skeleton and stamp your project identity in one step.${C_RESET}"

    while true; do
        printf '%s' "Go module path ${C_DIM}(e.g. github.com/acme/orders-api)${C_RESET}: "
        read -r NEW_MODULE || true
        if validate_module "$NEW_MODULE"; then
            break
        fi
        NEW_MODULE=""
    done

    local default_display
    default_display="$(derive_display "$(derive_slug "$NEW_MODULE")")"
    printf '%s' "Display name ${C_DIM}[${default_display}]${C_RESET}: "
    read -r NEW_DISPLAY || true
    [ -z "$NEW_DISPLAY" ] && NEW_DISPLAY="$default_display"

    local default_dir
    default_dir="./$(derive_slug "$NEW_MODULE")"
    printf '%s' "Target directory ${C_DIM}[${default_dir}]${C_RESET}: "
    read -r TARGET_DIR || true
    [ -z "$TARGET_DIR" ] && TARGET_DIR="$default_dir"
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

main() {
    parse_args "$@"
    preflight

    # Determine mode: interactive when no module path was given
    if [ -z "$NEW_MODULE" ]; then
        if [ ! -t 0 ]; then
            die "stdin is not a terminal and no module path was given.
Pass a module path as the first argument, or use the interactive form:
  bash <(curl -fsSL https://raw.githubusercontent.com/mr-kaynak/go-core/main/scripts/create.sh)"
        fi
        prompt_inputs
    else
        validate_module "$NEW_MODULE" || exit 1
        [ -z "$NEW_DISPLAY" ] && NEW_DISPLAY="$(derive_display "$(derive_slug "$NEW_MODULE")")"
        [ -z "$TARGET_DIR" ] && TARGET_DIR="./$(derive_slug "$NEW_MODULE")"
    fi

    # Expand a leading ./ so messages are readable
    local abs_target
    abs_target="$(cd "$(dirname "$TARGET_DIR")" 2>/dev/null && pwd)/$(basename "$TARGET_DIR")" \
        || abs_target="$TARGET_DIR"

    # Guard against an existing non-empty target
    if [ -d "$abs_target" ] && [ -n "$(ls -A "$abs_target" 2>/dev/null)" ]; then
        die "Target directory already exists and is non-empty: $abs_target
Remove it or choose a different path."
    fi

    hdr "Bootstrap plan"
    printf '  %-18s %s\n' "Module path:"  "${C_BOLD}${NEW_MODULE}${C_RESET}"
    printf '  %-18s %s\n' "Display name:" "${C_BOLD}${NEW_DISPLAY}${C_RESET}"
    printf '  %-18s %s\n' "Target dir:"   "${C_BOLD}${abs_target}${C_RESET}"
    printf '  %-18s %s\n' "Clone from:"   "${C_DIM}${GO_CORE_REPO}${C_RESET}"
    printf '\n'

    # ---------------------------------------------------------------------------
    # Clone
    # ---------------------------------------------------------------------------
    info "Cloning go-core skeleton..."
    git clone --depth 1 "$GO_CORE_REPO" "$abs_target"
    step "Cloned into $abs_target"

    # Remove skeleton's git history so init-project.sh can create a fresh one.
    # reset_git() inside init-project.sh does `rm -rf .git` then `git init`,
    # so removing it here is safe — it will just skip the rm and run git init.
    rm -rf "$abs_target/.git"
    step "Skeleton git history removed"

    # ---------------------------------------------------------------------------
    # Delegate to init-project.sh
    # ---------------------------------------------------------------------------
    info "Running init-project.sh..."
    # Pass module + display name as positional args (non-interactive mode in init).
    # init-project.sh will: rewrite identity, generate .env with random secrets,
    # run `git init` + initial commit, then verify with `go build` / `go vet`.
    bash "$abs_target/scripts/init-project.sh" "$NEW_MODULE" "$NEW_DISPLAY"

    # ---------------------------------------------------------------------------
    # Next steps
    # ---------------------------------------------------------------------------
    hdr "Done — ${NEW_DISPLAY} is ready"
    cat <<EOF
${C_BOLD}Next steps:${C_RESET}
  cd ${abs_target}
  \$EDITOR .env                           # review generated secrets
  make docker-up                         # start Postgres, Redis, RabbitMQ, Jaeger, etc.
  make migrate                           # run database migrations
  make run                               # start the API server (port 3000)
  open http://localhost:3000/docs        # interactive API docs (Scalar UI)

${C_DIM}Security note: review this script before piping to bash:
  https://github.com/mr-kaynak/go-core/blob/main/scripts/create.sh${C_RESET}
EOF
}

main "$@"
