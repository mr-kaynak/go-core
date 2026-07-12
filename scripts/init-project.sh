#!/bin/bash
#
# init-project.sh - Stamp out a brand-new project from the go-core skeleton.
#
# Run it with no arguments for an interactive wizard, or pass the module path
# (and optional display name) for non-interactive / CI use.
#
# Interactive:
#   ./scripts/init-project.sh
#
# Non-interactive:
#   ./scripts/init-project.sh github.com/acme/orders-api "Orders API"
#   ./scripts/init-project.sh github.com/acme/orders-api            # display name derived
#
# Flags:
#   --keep-git      Do NOT reset git history (keep the skeleton's .git as-is).
#   --no-verify     Skip the `go build` / `go vet` verification step.
#   -h, --help      Show usage.
#
# What it does:
#   1. Rewrites the Go module path across go.mod and every Go/proto/doc file.
#   2. Rewrites the project identity everywhere it is hardcoded: config
#      defaults, .env.example, Docker image/container names, RabbitMQ / OTEL /
#      JWT defaults, Prometheus + Grafana config, Swagger annotations, README,
#      CLAUDE.md, CONTRIBUTING.md, SECURITY.md, GitHub workflows, and the
#      startup banner.
#   3. Generates a real .env with cryptographically-random secrets.
#   4. Optionally resets git history and creates an initial commit.
#   5. Verifies the result compiles.
#
# The script refuses to run twice: once the module path is no longer the
# skeleton's, it exits with a friendly message.

set -euo pipefail

# --- Constants -------------------------------------------------------------

readonly OLD_MODULE="github.com/mr-kaynak/go-core"
readonly OLD_SLUG="go-core"           # kebab identity (docker, bucket, exchange, ...)
readonly OLD_NS="go_core"             # prometheus metrics namespace (snake_case)
readonly OLD_DISPLAY="Go-Core"        # human-facing display name
readonly OLD_GH_OWNER="mr-kaynak"     # github owner in URLs / GHCR

# --- Colors ----------------------------------------------------------------

if [ -t 1 ]; then
    C_RESET=$'\033[0m'; C_BOLD=$'\033[1m'
    C_RED=$'\033[31m'; C_GREEN=$'\033[32m'; C_YELLOW=$'\033[33m'
    C_BLUE=$'\033[34m'; C_CYAN=$'\033[36m'; C_DIM=$'\033[2m'
else
    C_RESET=''; C_BOLD=''; C_RED=''; C_GREEN=''; C_YELLOW=''
    C_BLUE=''; C_CYAN=''; C_DIM=''
fi

info()  { printf '%s\n' "${C_CYAN}==>${C_RESET} $*"; }
step()  { printf '%s\n' "  ${C_GREEN}OK${C_RESET} $*"; }
warn()  { printf '%s\n' "${C_YELLOW}!${C_RESET}  $*" >&2; }
err()   { printf '%s\n' "${C_RED}ERROR: $*${C_RESET}" >&2; }
die()   { err "$@"; exit 1; }
hdr()   { printf '\n%s\n' "${C_BOLD}${C_BLUE}$*${C_RESET}"; }

# --- Globals set during parsing -------------------------------------------

NEW_MODULE=""
NEW_DISPLAY=""
KEEP_GIT=0
DO_VERIFY=1
INTERACTIVE=0

PROJECT_ROOT=""
NEW_SLUG=""       # e.g. orders-api
NEW_NS=""         # e.g. orders_api  (prometheus namespace, snake_case, lowercase)
NEW_GH_OWNER=""   # e.g. acme
NEW_GH_PATH=""    # e.g. acme/orders-api (owner/repo, from module path)

# --- Portable in-place replace --------------------------------------------
# Uses perl -pi -e which behaves identically on macOS and Linux, unlike sed -i.
# $1 = literal search string, $2 = literal replacement, $3.. = files.
replace_in_files() {
    local search="$1" repl="$2"; shift 2
    local f
    for f in "$@"; do
        [ -f "$f" ] || continue
        SEARCH="$search" REPL="$repl" perl -pi -e '
            BEGIN { $s = $ENV{SEARCH}; $r = $ENV{REPL}; }
            s/\Q$s\E/$r/g;
        ' "$f"
    done
}

# Replace across a NUL-delimited file list read from stdin.
replace_in_stream() {
    local search="$1" repl="$2"
    local f
    while IFS= read -r -d '' f; do
        if grep -qF "$search" "$f" 2>/dev/null; then
            SEARCH="$search" REPL="$repl" perl -pi -e '
                BEGIN { $s = $ENV{SEARCH}; $r = $ENV{REPL}; }
                s/\Q$s\E/$r/g;
            ' "$f"
        fi
    done
}

# --- Derivations -----------------------------------------------------------

# slug from last path segment: github.com/acme/orders-api -> orders-api
derive_slug() {
    printf '%s' "${1##*/}"
}

# github owner/repo path: github.com/acme/orders-api -> acme/orders-api
derive_gh_path() {
    local mod="$1"
    case "$mod" in
        github.com/*) printf '%s' "${mod#github.com/}" ;;
        */*/*)        printf '%s' "${mod#*/}" ;;   # host/owner/repo -> owner/repo
        *)            printf '%s' "$mod" ;;
    esac
}

# prometheus namespace: kebab/space -> snake, lowercased, drop invalid chars.
# (sed for the space/dash->underscore step; `tr ' -'` is read as a char range.)
derive_ns() {
    printf '%s' "$1" \
        | tr '[:upper:]' '[:lower:]' \
        | sed 's/[ -]/_/g' \
        | sed 's/[^a-z0-9_]//g' \
        | sed 's/^[0-9]*//'   # namespace must not start with a digit
}

# display name from slug: orders-api -> Orders Api
# (sed instead of `tr '-_'` — a leading '-' in tr's set1 is read as a flag on macOS)
derive_display() {
    printf '%s' "$1" | sed 's/[-_]/ /g' | awk '{
        for (i = 1; i <= NF; i++) {
            $i = toupper(substr($i, 1, 1)) substr($i, 2)
        }
        print
    }'
}

# --- Validation ------------------------------------------------------------

validate_module() {
    local mod="$1"
    if [ -z "$mod" ]; then
        return 1
    fi
    if printf '%s' "$mod" | grep -qE '[[:space:]]'; then
        err "Module path must not contain spaces: '$mod'"
        return 1
    fi
    if ! printf '%s' "$mod" | grep -qE '^[a-zA-Z0-9][a-zA-Z0-9._-]*(/[a-zA-Z0-9._-]+)+$'; then
        err "Module path '$mod' does not look like a Go module (expected e.g. github.com/acme/orders-api)."
        return 1
    fi
    if [ "$mod" = "$OLD_MODULE" ]; then
        err "That is the skeleton's own module path. Choose a new one."
        return 1
    fi
    return 0
}

# --- Argument parsing ------------------------------------------------------

usage() {
    cat <<EOF
${C_BOLD}Initialize a new project from the go-core skeleton.${C_RESET}

${C_BOLD}Usage:${C_RESET}
  $0                                  # interactive wizard
  $0 <module-path> [display-name]     # non-interactive
  make init NAME=<module-path>        # via Makefile

${C_BOLD}Arguments:${C_RESET}
  module-path     Go module path, e.g. github.com/acme/orders-api
  display-name    Human name, e.g. "Orders API" (default: derived from module)

${C_BOLD}Flags:${C_RESET}
  --keep-git      Keep the skeleton's git history (default: fresh git init)
  --no-verify     Skip go build / go vet verification
  -h, --help      Show this help
EOF
}

parse_args() {
    local positional=()
    while [ $# -gt 0 ]; do
        case "$1" in
            -h|--help)   usage; exit 0 ;;
            --keep-git)  KEEP_GIT=1; shift ;;
            --no-verify) DO_VERIFY=0; shift ;;
            --) shift; while [ $# -gt 0 ]; do positional+=("$1"); shift; done ;;
            -*) die "Unknown flag: $1 (use --help)" ;;
            *)  positional+=("$1"); shift ;;
        esac
    done
    if [ ${#positional[@]} -ge 1 ]; then
        NEW_MODULE="${positional[0]}"
    fi
    if [ ${#positional[@]} -ge 2 ]; then
        NEW_DISPLAY="${positional[1]}"
    fi
}

# --- Interactive prompts ---------------------------------------------------

prompt_inputs() {
    INTERACTIVE=1
    hdr "go-core project initializer"
    printf '%s\n\n' "${C_DIM}Answer a couple of questions and this skeleton becomes your project.${C_RESET}"

    while [ -z "$NEW_MODULE" ]; do
        printf '%s' "Go module path ${C_DIM}(e.g. github.com/acme/orders-api)${C_RESET}: "
        read -r NEW_MODULE || true
        if ! validate_module "$NEW_MODULE"; then
            NEW_MODULE=""
        fi
    done

    local default_display
    default_display="$(derive_display "$(derive_slug "$NEW_MODULE")")"
    printf '%s' "Display name ${C_DIM}[${default_display}]${C_RESET}: "
    read -r NEW_DISPLAY || true
    [ -z "$NEW_DISPLAY" ] && NEW_DISPLAY="$default_display"

    if [ "$KEEP_GIT" -eq 0 ]; then
        printf '%s' "Reset git history and create a fresh initial commit? ${C_DIM}[Y/n]${C_RESET}: "
        local ans; read -r ans || true
        case "$ans" in [nN]*) KEEP_GIT=1 ;; esac
    fi
}

confirm_plan() {
    hdr "Replacement plan"
    printf '  %-22s %s\n' "Module path:"      "${C_BOLD}${NEW_MODULE}${C_RESET}"
    printf '  %-22s %s\n' "Display name:"     "${C_BOLD}${NEW_DISPLAY}${C_RESET}"
    printf '  %-22s %s\n' "Project slug:"     "$NEW_SLUG"
    printf '  %-22s %s\n' "Metrics namespace:" "$NEW_NS"
    printf '  %-22s %s\n' "GitHub path:"      "$NEW_GH_PATH"
    printf '  %-22s %s\n' "Git history:"      "$([ "$KEEP_GIT" -eq 1 ] && echo 'kept' || echo 'reset (fresh commit)')"
    printf '  %-22s %s\n' "Verify build:"     "$([ "$DO_VERIFY" -eq 1 ] && echo 'yes' || echo 'skipped')"
    echo ""
    if [ "$INTERACTIVE" -eq 1 ]; then
        printf '%s' "Proceed? ${C_DIM}[Y/n]${C_RESET}: "
        local ans; read -r ans || true
        case "$ans" in [nN]*) die "Aborted." ;; esac
    fi
}

# --- Rename sweep ----------------------------------------------------------

sweep_go_and_proto() {
    info "Rewriting Go module path ($OLD_MODULE -> $NEW_MODULE)"
    replace_in_files "$OLD_MODULE" "$NEW_MODULE" "$PROJECT_ROOT/go.mod"
    # IMPORTANT: exclude *.pb.go. In generated protobuf code the module path only
    # appears inside the length-prefixed rawDesc byte string (the go_package
    # option); a plain text replace there desyncs the length prefix and panics at
    # init. The .proto go_package option IS updated below so a future
    # `make proto` regenerates correct paths.
    find "$PROJECT_ROOT" \( -name '*.go' -o -name '*.proto' \) \
        -not -name '*.pb.go' \
        -not -path '*/vendor/*' -not -path '*/.git/*' -print0 \
        | replace_in_stream "$OLD_MODULE" "$NEW_MODULE"
    # Generated swagger docs embed the module path with '.' and '/' turned to '_'.
    local doc_mod_old doc_mod_new
    doc_mod_old="$(printf '%s' "$OLD_MODULE" | tr './' '__')"
    doc_mod_new="$(printf '%s' "$NEW_MODULE" | tr './' '__')"
    for docfile in docs/docs.go docs/openapi.json docs/openapi.yaml; do
        replace_in_files "$OLD_MODULE" "$NEW_MODULE" "$PROJECT_ROOT/$docfile"
        replace_in_files "$doc_mod_old" "$doc_mod_new" "$PROJECT_ROOT/$docfile"
        replace_in_files "$OLD_DISPLAY API" "$NEW_DISPLAY API" "$PROJECT_ROOT/$docfile"
        replace_in_files "$OLD_DISPLAY Team" "$NEW_DISPLAY Team" "$PROJECT_ROOT/$docfile"
        replace_in_files "github.com/$OLD_GH_OWNER/$OLD_SLUG" "github.com/$NEW_GH_PATH" "$PROJECT_ROOT/$docfile"
    done
    step "Module path + imports + generated docs rewritten"
}

sweep_config_defaults() {
    info "Rewriting config defaults, .env.example, and config_test.go"
    local cfg="$PROJECT_ROOT/internal/core/config/config.go"
    local env="$PROJECT_ROOT/.env.example"
    local cfg_test="$PROJECT_ROOT/internal/core/config/config_test.go"

    # config.go hardcoded defaults
    replace_in_files "\"app.name\", \"$OLD_SLUG\"" "\"app.name\", \"$NEW_SLUG\"" "$cfg"
    replace_in_files "\"storage.s3_bucket\", \"$OLD_SLUG\"" "\"storage.s3_bucket\", \"$NEW_SLUG\"" "$cfg"

    # .env.example values
    replace_in_files "APP_NAME=$OLD_SLUG"                     "APP_NAME=$NEW_SLUG"                     "$env"
    replace_in_files "DB_NAME=${OLD_NS}_dev"                 "DB_NAME=${NEW_NS}_dev"                 "$env"
    replace_in_files "RABBITMQ_EXCHANGE=${OLD_SLUG}-exchange" "RABBITMQ_EXCHANGE=${NEW_SLUG}-exchange" "$env"
    replace_in_files "RABBITMQ_QUEUE_PREFIX=$OLD_SLUG"       "RABBITMQ_QUEUE_PREFIX=$NEW_SLUG"       "$env"
    replace_in_files "JWT_ISSUER=$OLD_SLUG"                  "JWT_ISSUER=$NEW_SLUG"                  "$env"
    replace_in_files "SMTP_FROM_NAME=$OLD_DISPLAY"           "SMTP_FROM_NAME=$NEW_DISPLAY"           "$env"
    replace_in_files "OTEL_SERVICE_NAME=$OLD_SLUG"           "OTEL_SERVICE_NAME=$NEW_SLUG"           "$env"
    replace_in_files "STORAGE_S3_BUCKET=$OLD_SLUG"           "STORAGE_S3_BUCKET=$NEW_SLUG"           "$env"

    # config_test.go asserts the default app.name and seeds identity-derived env
    # values; without this the stamped project's `go test` would fail.
    replace_in_files "\"DB_NAME\", \"${OLD_NS}_test\"" "\"DB_NAME\", \"${NEW_NS}_test\"" "$cfg_test"
    replace_in_files "\"RABBITMQ_EXCHANGE\", \"$OLD_SLUG\"" "\"RABBITMQ_EXCHANGE\", \"$NEW_SLUG\"" "$cfg_test"
    replace_in_files "\"RABBITMQ_QUEUE_PREFIX\", \"$OLD_SLUG\"" "\"RABBITMQ_QUEUE_PREFIX\", \"$NEW_SLUG\"" "$cfg_test"
    replace_in_files "\"JWT_ISSUER\", \"${OLD_SLUG}-tests\"" "\"JWT_ISSUER\", \"${NEW_SLUG}-tests\"" "$cfg_test"
    replace_in_files "loaded.App.Name != \"$OLD_SLUG\"" "loaded.App.Name != \"$NEW_SLUG\"" "$cfg_test"
    replace_in_files "expected default app name $OLD_SLUG" "expected default app name $NEW_SLUG" "$cfg_test"
    step "config.go + .env.example + config_test.go rewritten"
}

sweep_cmd_identity() {
    info "Rewriting entrypoint identity (banners, swagger, gRPC defaults)"
    local api="$PROJECT_ROOT/cmd/api/main.go"
    local grpc="$PROJECT_ROOT/cmd/grpc/main.go"

    # Swagger annotations in cmd/api/main.go
    replace_in_files "// @title $OLD_DISPLAY API"          "// @title $NEW_DISPLAY API"          "$api"
    replace_in_files "// @contact.name $OLD_DISPLAY Team"  "// @contact.name $NEW_DISPLAY Team"  "$api"
    replace_in_files "// @contact.url https://github.com/$OLD_GH_OWNER/$OLD_SLUG" \
                     "// @contact.url https://github.com/$NEW_GH_PATH" "$api"
    replace_in_files "Starting $OLD_DISPLAY API Server"    "Starting $NEW_DISPLAY API Server"    "$api"

    # gRPC-specific defaults
    replace_in_files "cfg.App.Name == \"$OLD_SLUG\""       "cfg.App.Name == \"$NEW_SLUG\""       "$grpc"
    replace_in_files "cfg.App.Name = \"$OLD_DISPLAY gRPC\"" "cfg.App.Name = \"$NEW_DISPLAY gRPC\"" "$grpc"
    replace_in_files "cfg.JWT.Issuer = \"${OLD_SLUG}-grpc\"" "cfg.JWT.Issuer = \"${NEW_SLUG}-grpc\"" "$grpc"
    replace_in_files "cfg.Tracing.ServiceName = \"${OLD_SLUG}-grpc\"" "cfg.Tracing.ServiceName = \"${NEW_SLUG}-grpc\"" "$grpc"
    replace_in_files "metrics.InitMetrics(\"$OLD_NS\")"    "metrics.InitMetrics(\"$NEW_NS\")"    "$grpc"

    # Server: Scalar docs title
    replace_in_files "Title:             \"$OLD_DISPLAY API\"" "Title:             \"$NEW_DISPLAY API\"" \
        "$PROJECT_ROOT/internal/infrastructure/server/server.go"

    # Event dispatcher default source
    replace_in_files "Source:        \"$OLD_SLUG\"" "Source:        \"$NEW_SLUG\"" \
        "$PROJECT_ROOT/internal/infrastructure/messaging/events/event_dispatcher.go"

    # Webhook User-Agent (source + test)
    replace_in_files "\"${OLD_SLUG}-webhook/1.0\"" "\"${NEW_SLUG}-webhook/1.0\"" \
        "$PROJECT_ROOT/internal/infrastructure/webhook/webhook_service.go" \
        "$PROJECT_ROOT/internal/infrastructure/webhook/webhook_service_test.go"

    replace_the_banner "$api"
    step "Entrypoints, banner, and swagger rewritten"
}

# Replace the multi-line ASCII banner block with a plain one-liner.
replace_the_banner() {
    local api="$1"
    DISPLAY="$NEW_DISPLAY" perl -0777 -pi -e '
        my $d = $ENV{DISPLAY};
        s/func printBanner\(\) \{.*?\n\}/func printBanner() {\n\tfmt.Printf("\\n  $d — starting up\\n\\n")\n}/s;
    ' "$api"
}

sweep_docker() {
    info "Rewriting Docker + Makefile image names"
    local files=(
        "$PROJECT_ROOT/docker-compose.yml"
        "$PROJECT_ROOT/docker-compose.prod.yml"
    )
    # GHCR paths first (owner/repo), then bare slug identifiers.
    for f in "${files[@]}"; do
        replace_in_files "ghcr.io/$OLD_GH_OWNER/$OLD_SLUG" "ghcr.io/$NEW_GH_PATH" "$f"
        replace_in_files "$OLD_SLUG" "$NEW_SLUG" "$f"
    done

    # Makefile: registry + image name + binary names + version banner.
    local mk="$PROJECT_ROOT/Makefile"
    replace_in_files "DOCKER_REGISTRY?=ghcr.io/$OLD_GH_OWNER" "DOCKER_REGISTRY?=ghcr.io/$NEW_GH_OWNER" "$mk"
    replace_in_files "DOCKER_IMAGE_NAME?=$OLD_SLUG" "DOCKER_IMAGE_NAME?=$NEW_SLUG" "$mk"
    replace_in_files "BINARY_API=${OLD_SLUG}-api" "BINARY_API=${NEW_SLUG}-api" "$mk"
    replace_in_files "BINARY_GRPC=${OLD_SLUG}-grpc" "BINARY_GRPC=${NEW_SLUG}-grpc" "$mk"
    replace_in_files "BINARY_MIGRATE=${OLD_SLUG}-migrate" "BINARY_MIGRATE=${NEW_SLUG}-migrate" "$mk"
    replace_in_files "$OLD_DISPLAY Boilerplate" "$NEW_DISPLAY" "$mk"
    step "Docker + Makefile image identity rewritten"
}

sweep_observability() {
    info "Rewriting Prometheus + Grafana config"
    # Prometheus job/target names reference the docker container/service names.
    replace_in_files "$OLD_SLUG" "$NEW_SLUG" "$PROJECT_ROOT/configs/prometheus.yml"

    # Grafana dashboard: metric names use the namespace (snake), everything else
    # (job labels, tags, uid, datasource, title) uses the slug/display identity.
    local dash="$PROJECT_ROOT/configs/grafana/dashboards/${OLD_SLUG}-overview.json"
    if [ -f "$dash" ]; then
        replace_in_files "${OLD_NS}_" "${NEW_NS}_" "$dash"        # metric namespace first
        replace_in_files "$OLD_DISPLAY Overview" "$NEW_DISPLAY Overview" "$dash"
        replace_in_files "$OLD_SLUG" "$NEW_SLUG" "$dash"          # job/tags/uid/datasource
        git -C "$PROJECT_ROOT" mv "$dash" \
            "$PROJECT_ROOT/configs/grafana/dashboards/${NEW_SLUG}-overview.json" 2>/dev/null \
            || mv "$dash" "$PROJECT_ROOT/configs/grafana/dashboards/${NEW_SLUG}-overview.json"
    fi
    replace_in_files "name: '$OLD_SLUG'" "name: '$NEW_SLUG'" \
        "$PROJECT_ROOT/configs/grafana/dashboards/dashboard.yml"
    replace_in_files "$OLD_SLUG" "$NEW_SLUG" \
        "$PROJECT_ROOT/configs/grafana/datasources/datasource.yml"
    step "Observability config rewritten"
}

sweep_github() {
    info "Rewriting GitHub workflows + issue templates"
    find "$PROJECT_ROOT/.github" -type f \( -name '*.yml' -o -name '*.yaml' -o -name '*.md' \) -print0 2>/dev/null \
        | replace_in_stream "$OLD_MODULE" "$NEW_MODULE"
    find "$PROJECT_ROOT/.github" -type f \( -name '*.yml' -o -name '*.yaml' -o -name '*.md' \) -print0 2>/dev/null \
        | replace_in_stream "$OLD_GH_OWNER/$OLD_SLUG" "$NEW_GH_PATH"
    # Release artifact names + the bare "go-core version" label in the bug template.
    find "$PROJECT_ROOT/.github" -type f \( -name '*.yml' -o -name '*.yaml' -o -name '*.md' \) -print0 2>/dev/null \
        | replace_in_stream "$OLD_SLUG" "$NEW_SLUG"
    step "GitHub config rewritten"
}

sweep_test_fixtures() {
    info "Rewriting test fixture identity constants"
    # These suffixes are specific enough to sweep across ALL Go files safely
    # (test helpers like internal/test/helpers.go are not *_test.go).
    #   e.g. Issuer: "go-core-test", User-Agent "go-core-webhook/1.0"
    find "$PROJECT_ROOT" -name '*.go' -not -path '*/vendor/*' -print0 \
        | replace_in_stream "${OLD_SLUG}-test" "${NEW_SLUG}-test"
    find "$PROJECT_ROOT" -name '*.go' -not -path '*/vendor/*' -print0 \
        | replace_in_stream "${OLD_SLUG}-webhook" "${NEW_SLUG}-webhook"
    # e.g. Name: "Go-Core Test"
    find "$PROJECT_ROOT" -name '*.go' -not -path '*/vendor/*' -print0 \
        | replace_in_stream "$OLD_DISPLAY Test" "$NEW_DISPLAY Test"
    step "Test fixtures rewritten"
}

sweep_docs() {
    info "Rewriting CLAUDE.md, CONTRIBUTING.md, SECURITY.md, README.md"

    # --- CLAUDE.md: keep ALL content (conventions + invariants verbatim),
    # rewrite only the identity strings. The "Initializing a New Project"
    # section is skeleton-only meta-documentation full of literal old-identity
    # examples; it is dropped wholesale (the guard blocks re-running init
    # anyway) rather than sed'd, which would leave it self-contradictory.
    local claude="$PROJECT_ROOT/CLAUDE.md"
    perl -0777 -pi -e 's/\n## Initializing a New Project\n.*?(?=\n## )/\n/s' "$claude"
    replace_in_files "# $OLD_SLUG"$'\n' "# $NEW_SLUG"$'\n' "$claude"
    replace_in_files "$OLD_MODULE" "$NEW_MODULE" "$claude"
    replace_in_files "$OLD_GH_OWNER/$OLD_SLUG" "$NEW_GH_PATH" "$claude"
    replace_in_files "\`$OLD_NS\`" "\`$NEW_NS\`" "$claude"

    # --- CONTRIBUTING.md / SECURITY.md: repo URLs + owner path ---
    for f in "$PROJECT_ROOT/CONTRIBUTING.md" "$PROJECT_ROOT/SECURITY.md"; do
        replace_in_files "$OLD_GH_OWNER/$OLD_SLUG" "$NEW_GH_PATH" "$f"
        replace_in_files "Contributing to $OLD_SLUG" "Contributing to $NEW_SLUG" "$f"
        replace_in_files "contributing to $OLD_SLUG" "contributing to $NEW_SLUG" "$f"
        replace_in_files "/$OLD_SLUG.git" "/$NEW_SLUG.git" "$f"
        replace_in_files "cd $OLD_SLUG" "cd $NEW_SLUG" "$f"
    done

    # --- README: move skeleton README aside, generate a fresh minimal one. ---
    if [ -f "$PROJECT_ROOT/README.md" ] && grep -qF "# $OLD_DISPLAY" "$PROJECT_ROOT/README.md"; then
        mkdir -p "$PROJECT_ROOT/docs"
        mv "$PROJECT_ROOT/README.md" "$PROJECT_ROOT/docs/SKELETON.md"
        generate_readme "$PROJECT_ROOT/README.md"
        step "Fresh README.md generated (skeleton archived to docs/SKELETON.md)"
    fi
    step "Docs rewritten"
}

generate_readme() {
    local out="$1"
    cat > "$out" <<EOF
# $NEW_DISPLAY

Built on the [go-core](https://github.com/$OLD_GH_OWNER/$OLD_SLUG) skeleton — a production-ready
enterprise Go application foundation (Fiber v3, gRPC, PostgreSQL/GORM, Redis, RabbitMQ,
Casbin RBAC, JWT + 2FA, Prometheus + OpenTelemetry).

Module: \`$NEW_MODULE\`

## Quick Start

\`\`\`bash
# 1. Start infrastructure (Postgres, Redis, RabbitMQ, Jaeger, Prometheus, Grafana)
make docker-up

# 2. Review your generated .env (secrets already filled in)
\$EDITOR .env

# 3. Run migrations
make migrate

# 4. Run the API server (port 3000)
make run
\`\`\`

API docs (Scalar UI): http://localhost:3000/docs

## Common Commands

| Command | Description |
|---------|-------------|
| \`make run\` | Run the API server |
| \`make run-grpc\` | Run the gRPC server |
| \`make test\` | Run all tests |
| \`make lint\` | Run golangci-lint |
| \`make migrate\` | Apply pending migrations |
| \`make migrate-create NAME=x\` | Create a new migration |
| \`make swagger\` | Regenerate OpenAPI docs |
| \`make docker-up\` / \`make docker-down\` | Start / stop infrastructure |

See [\`CLAUDE.md\`](./CLAUDE.md) for architecture, conventions, and the module-authoring
guide. The full skeleton reference lives in [\`docs/SKELETON.md\`](./docs/SKELETON.md).

## License

See [LICENSE](./LICENSE).
EOF
}

# --- Secrets / .env --------------------------------------------------------

gen_secret() {
    # base64 of 48 random bytes -> 64 chars, comfortably above the 32-char minimum.
    openssl rand -base64 48 | tr -d '\n'
}

generate_env() {
    hdr "Generating .env"
    local env_file="$PROJECT_ROOT/.env"
    if [ -f "$env_file" ]; then
        if [ "$INTERACTIVE" -eq 1 ]; then
            printf '%s' ".env already exists. Overwrite? ${C_DIM}[y/N]${C_RESET}: "
            local ans; read -r ans || true
            case "$ans" in [yY]*) ;; *) warn "Kept existing .env — secrets NOT regenerated."; return ;; esac
        else
            warn ".env already exists — leaving it untouched (pass a clean tree for fresh secrets)."
            return
        fi
    fi

    if ! command -v openssl >/dev/null 2>&1; then
        warn "openssl not found — copying .env.example without generating secrets."
        cp "$PROJECT_ROOT/.env.example" "$env_file"
        return
    fi

    cp "$PROJECT_ROOT/.env.example" "$env_file"
    local jwt refresh enc
    jwt="$(gen_secret)"; refresh="$(gen_secret)"; enc="$(gen_secret)"

    set_env_var "$env_file" "JWT_SECRET" "$jwt"
    set_env_var "$env_file" "JWT_REFRESH_SECRET" "$refresh"
    set_env_var "$env_file" "SECURITY_ENCRYPTION_KEY" "$enc"
    step ".env created with random JWT + encryption secrets"
}

# Set KEY=value in an env file (replaces the line if present, appends otherwise).
set_env_var() {
    local file="$1" key="$2" val="$3"
    if grep -qE "^${key}=" "$file"; then
        KEY="$key" VAL="$val" perl -pi -e '
            BEGIN { $k = $ENV{KEY}; $v = $ENV{VAL}; }
            s/^\Q$k\E=.*$/"$k=$v"/e;
        ' "$file"
    else
        printf '%s=%s\n' "$key" "$val" >> "$file"
    fi
}

# --- Git -------------------------------------------------------------------

reset_git() {
    [ "$KEEP_GIT" -eq 1 ] && { info "Keeping existing git history (--keep-git)"; return; }
    hdr "Resetting git history"
    if ! command -v git >/dev/null 2>&1; then
        warn "git not found — skipping git reset."
        return
    fi
    rm -rf "$PROJECT_ROOT/.git"
    ( cd "$PROJECT_ROOT" && git init -q && git add -A \
        && git commit -q -m "chore: initialize $NEW_DISPLAY from go-core skeleton" )
    step "Fresh git repository initialized with an initial commit"
}

# --- Verification ----------------------------------------------------------

verify_build() {
    [ "$DO_VERIFY" -eq 0 ] && { warn "Skipping verification (--no-verify)"; return 0; }
    hdr "Verifying the result compiles"
    if ! command -v go >/dev/null 2>&1; then
        warn "go toolchain not found — skipping build verification."
        return 0
    fi
    ( cd "$PROJECT_ROOT" && go mod tidy ) >/dev/null 2>&1 || warn "go mod tidy reported issues (continuing)."

    local ok=1
    info "go build ./..."
    if ( cd "$PROJECT_ROOT" && go build ./... ) 2>"$PROJECT_ROOT/.init-build.log"; then
        step "go build passed"
    else
        ok=0
        err "go build FAILED. See output below:"
        cat "$PROJECT_ROOT/.init-build.log" >&2
    fi
    info "go vet ./..."
    if ( cd "$PROJECT_ROOT" && go vet ./... ) 2>"$PROJECT_ROOT/.init-vet.log"; then
        step "go vet passed"
    else
        ok=0
        err "go vet FAILED. See output below:"
        cat "$PROJECT_ROOT/.init-vet.log" >&2
    fi
    rm -f "$PROJECT_ROOT/.init-build.log" "$PROJECT_ROOT/.init-vet.log"

    if [ "$ok" -eq 0 ]; then
        warn "Verification failed. Common causes: a leftover reference to the old module"
        warn "path, or a stale docs/*.go. Run 'make swagger' to regenerate docs, then"
        warn "grep -r '$OLD_MODULE' . to find anything the sweep missed."
        return 1
    fi
    return 0
}

# --- Optional protobuf regeneration ---------------------------------------
# The .proto go_package option now points at the new module, but the checked-in
# *.pb.go still embeds the old path in its (harmless, codegen-only) rawDesc.
# Regenerate to make them consistent — but only if the proto toolchain exists.
regen_proto() {
    if command -v protoc >/dev/null 2>&1 && [ -f "$PROJECT_ROOT/Makefile" ] \
        && grep -qE '^proto:' "$PROJECT_ROOT/Makefile"; then
        info "Regenerating protobuf code (protoc found)"
        if ( cd "$PROJECT_ROOT" && make proto ) >/dev/null 2>&1; then
            step "Protobuf code regenerated"
        else
            warn "make proto failed — .pb.go keeps the old go_package (harmless at runtime)."
        fi
    else
        warn "protoc not found — skipping proto regen. The generated .pb.go files keep"
        warn "the skeleton's go_package path in their descriptor (harmless at runtime;"
        warn "run 'make proto' later to make them consistent)."
    fi
}

# --- Guard against double-run ---------------------------------------------

guard_already_initialized() {
    local current
    current="$(grep -E '^module ' "$PROJECT_ROOT/go.mod" | awk '{print $2}')"
    if [ "$current" != "$OLD_MODULE" ]; then
        hdr "Already initialized"
        printf '%s\n' "This project's module path is already ${C_BOLD}${current}${C_RESET}, not the skeleton's."
        printf '%s\n' "${C_DIM}init-project.sh only runs against a pristine go-core checkout — nothing to do.${C_RESET}"
        exit 0
    fi
}

# --- Next steps ------------------------------------------------------------

print_next_steps() {
    hdr "Done — $NEW_DISPLAY is ready"
    cat <<EOF
${C_BOLD}Next steps:${C_RESET}
  1. Review your secrets:        ${C_CYAN}\$EDITOR .env${C_RESET}
  2. Start infrastructure:       ${C_CYAN}make docker-up${C_RESET}
  3. Run migrations:             ${C_CYAN}make migrate${C_RESET}
  4. Run the API server:         ${C_CYAN}make run${C_RESET}
  5. Open API docs:              ${C_CYAN}http://localhost:3000/docs${C_RESET}

${C_DIM}Notes:${C_RESET}
  • LICENSE was left unchanged — update the copyright holder if needed.
  • The gRPC proto package name (gocore.v1) is a wire identifier and was left
    as-is. To rename it, edit the 'package' line in api/proto/*.proto, then
    run 'make proto'.
  • The .proto go_package now points at your module; regenerate .pb.go with
    'make proto' if it was skipped above.
  • The skeleton README is preserved at docs/SKELETON.md for reference.
EOF
}

# --- Main ------------------------------------------------------------------

main() {
    local script_dir
    script_dir="$(cd "$(dirname "$0")" && pwd)"
    PROJECT_ROOT="$(cd "$script_dir/.." && pwd)"
    [ -f "$PROJECT_ROOT/go.mod" ] || die "go.mod not found in $PROJECT_ROOT — run this from the skeleton root."

    parse_args "$@"
    guard_already_initialized

    if [ -z "$NEW_MODULE" ]; then
        prompt_inputs
    else
        validate_module "$NEW_MODULE" || die "Invalid module path."
        [ -z "$NEW_DISPLAY" ] && NEW_DISPLAY="$(derive_display "$(derive_slug "$NEW_MODULE")")"
    fi

    NEW_SLUG="$(derive_slug "$NEW_MODULE")"
    NEW_NS="$(derive_ns "$NEW_SLUG")"
    NEW_GH_PATH="$(derive_gh_path "$NEW_MODULE")"
    NEW_GH_OWNER="${NEW_GH_PATH%%/*}"

    [ -n "$NEW_NS" ] || die "Could not derive a valid Prometheus namespace from '$NEW_SLUG'."

    confirm_plan

    hdr "Rewriting project identity"
    sweep_go_and_proto
    sweep_config_defaults
    sweep_cmd_identity
    sweep_docker
    sweep_observability
    sweep_github
    sweep_test_fixtures
    sweep_docs
    regen_proto

    generate_env
    reset_git

    local verify_rc=0
    verify_build || verify_rc=$?

    print_next_steps
    return "$verify_rc"
}

main "$@"
