#!/bin/bash
#
# init-project.sh - Initialize a new project from the go-core boilerplate.
#
# Replaces the default Go module path (github.com/mr-kaynak/go-core) with a
# custom module name across go.mod, all Go source files, proto files, and
# generated documentation.
#
# Usage:
#   ./scripts/init-project.sh <new-module-name>
#
# Example:
#   ./scripts/init-project.sh github.com/yourcompany/myproject

set -euo pipefail

OLD_MODULE="github.com/mr-kaynak/go-core"
NEW_MODULE="${1:-}"

# --- Validation ---

if [ -z "$NEW_MODULE" ]; then
    echo "Error: module name is required."
    echo ""
    echo "Usage: $0 <new-module-name>"
    echo "Example: $0 github.com/yourcompany/myproject"
    exit 1
fi

if [ "$NEW_MODULE" = "$OLD_MODULE" ]; then
    echo "Error: new module name is the same as the current one ($OLD_MODULE)."
    exit 1
fi

# Validate the module name looks reasonable (contains at least one slash)
if ! echo "$NEW_MODULE" | grep -q '/'; then
    echo "Warning: module name '$NEW_MODULE' does not contain a '/' — this is unusual for Go modules."
    echo "Expected format: github.com/owner/repo"
    read -r -p "Continue anyway? [y/N] " confirm
    if [ "$confirm" != "y" ] && [ "$confirm" != "Y" ]; then
        echo "Aborted."
        exit 1
    fi
fi

# --- Determine project root ---

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

if [ ! -f "$PROJECT_ROOT/go.mod" ]; then
    echo "Error: go.mod not found in $PROJECT_ROOT. Are you running this from the right directory?"
    exit 1
fi

echo "Project root: $PROJECT_ROOT"
echo "Replacing: $OLD_MODULE"
echo "     With: $NEW_MODULE"
echo ""

# --- Perform replacements ---

# Escape slashes and dots for sed
OLD_ESCAPED=$(echo "$OLD_MODULE" | sed 's/[.[\/*^$]/\\&/g')
NEW_ESCAPED=$(echo "$NEW_MODULE" | sed 's/[.[\/*^$]/\\&/g')

changed_count=0

# Replace in go.mod
if [ -f "$PROJECT_ROOT/go.mod" ]; then
    if grep -q "$OLD_MODULE" "$PROJECT_ROOT/go.mod"; then
        sed -i '' "s|$OLD_MODULE|$NEW_MODULE|g" "$PROJECT_ROOT/go.mod"
        echo "  Updated: go.mod"
        changed_count=$((changed_count + 1))
    fi
fi

# Replace in all .go files
while IFS= read -r -d '' file; do
    if grep -q "$OLD_MODULE" "$file"; then
        sed -i '' "s|$OLD_MODULE|$NEW_MODULE|g" "$file"
        echo "  Updated: ${file#$PROJECT_ROOT/}"
        changed_count=$((changed_count + 1))
    fi
done < <(find "$PROJECT_ROOT" -name '*.go' -not -path '*/vendor/*' -not -path '*/.git/*' -print0)

# Replace in .proto files
while IFS= read -r -d '' file; do
    if grep -q "$OLD_MODULE" "$file"; then
        sed -i '' "s|$OLD_MODULE|$NEW_MODULE|g" "$file"
        echo "  Updated: ${file#$PROJECT_ROOT/}"
        changed_count=$((changed_count + 1))
    fi
done < <(find "$PROJECT_ROOT" -name '*.proto' -not -path '*/vendor/*' -not -path '*/.git/*' -print0)

# Replace in generated doc files (openapi.json, openapi.yaml, docs.go)
for docfile in "$PROJECT_ROOT/docs/docs.go" "$PROJECT_ROOT/docs/openapi.json" "$PROJECT_ROOT/docs/openapi.yaml"; do
    if [ -f "$docfile" ] && grep -q "$OLD_MODULE" "$docfile"; then
        sed -i '' "s|$OLD_MODULE|$NEW_MODULE|g" "$docfile"
        echo "  Updated: ${docfile#$PROJECT_ROOT/}"
        changed_count=$((changed_count + 1))
    fi
done

# Replace in Dockerfile (if referenced)
if [ -f "$PROJECT_ROOT/Dockerfile" ] && grep -q "$OLD_MODULE" "$PROJECT_ROOT/Dockerfile"; then
    sed -i '' "s|$OLD_MODULE|$NEW_MODULE|g" "$PROJECT_ROOT/Dockerfile"
    echo "  Updated: Dockerfile"
    changed_count=$((changed_count + 1))
fi

# Replace in docker-compose files (if referenced)
while IFS= read -r -d '' file; do
    if grep -q "$OLD_MODULE" "$file"; then
        sed -i '' "s|$OLD_MODULE|$NEW_MODULE|g" "$file"
        echo "  Updated: ${file#$PROJECT_ROOT/}"
        changed_count=$((changed_count + 1))
    fi
done < <(find "$PROJECT_ROOT" -maxdepth 1 -name 'docker-compose*.yml' -print0 2>/dev/null)

# Replace in Makefile (if the full module path is referenced)
if [ -f "$PROJECT_ROOT/Makefile" ] && grep -q "$OLD_MODULE" "$PROJECT_ROOT/Makefile"; then
    sed -i '' "s|$OLD_MODULE|$NEW_MODULE|g" "$PROJECT_ROOT/Makefile"
    echo "  Updated: Makefile"
    changed_count=$((changed_count + 1))
fi

# Replace in GitHub Actions workflows
while IFS= read -r -d '' file; do
    if grep -q "$OLD_MODULE" "$file"; then
        sed -i '' "s|$OLD_MODULE|$NEW_MODULE|g" "$file"
        echo "  Updated: ${file#$PROJECT_ROOT/}"
        changed_count=$((changed_count + 1))
    fi
done < <(find "$PROJECT_ROOT/.github" -name '*.yml' -print0 2>/dev/null)

# --- Summary ---

echo ""
if [ "$changed_count" -eq 0 ]; then
    echo "No files contained '$OLD_MODULE'. Nothing was changed."
else
    echo "Done. Updated $changed_count file(s)."
    echo ""
    echo "Next steps:"
    echo "  1. Run 'go mod tidy' to verify dependencies"
    echo "  2. Run 'make build' to verify compilation"
    echo "  3. Run 'make test' to verify tests pass"
fi
