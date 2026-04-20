#!/usr/bin/env bash
#
# Release script for AxonHub
# Usage: ./scripts/release/release.sh [patch|minor|major] [--dry-run]
#
# This script:
#   1. Reads current version from internal/build/VERSION
#   2. Bumps the version (patch by default)
#   3. Updates internal/build/VERSION
#   4. Creates and pushes a git tag
#   5. GitHub Actions handles the rest (build + release to GitHub)
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
VERSION_FILE="$REPO_ROOT/internal/build/VERSION"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() { echo -e "${GREEN}[INFO]${NC} $*"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }

usage() {
    cat <<EOF
Usage: $(basename "$0") [patch|minor|major] [--dry-run]

Bump the version and create a release tag.

Arguments:
  patch    Bump patch version (e.g., v0.9.35 -> v0.9.36) [default]
  minor    Bump minor version (e.g., v0.9.35 -> v0.10.0)
  major    Bump major version (e.g., v0.9.35 -> v1.0.0)

Options:
  --dry-run    Show what would be done without making changes

Examples:
  $(basename "$0")           # Bump patch: v0.9.35 -> v0.9.36
  $(basename "$0") minor     # Bump minor: v0.9.35 -> v0.10.0
  $(basename "$0") major     # Bump major: v0.9.35 -> v1.0.0
  $(basename "$0") --dry-run # Preview without changes
EOF
}

# Parse arguments
BUMP_TYPE="patch"
DRY_RUN=false

while [[ $# -gt 0 ]]; do
    case $1 in
        patch|minor|major)
            BUMP_TYPE="$1"
            shift
            ;;
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            log_error "Unknown argument: $1"
            usage
            exit 1
            ;;
    esac
done

cd "$REPO_ROOT"

# Check for uncommitted changes
if ! git diff --quiet; then
    log_error "You have uncommitted changes. Please commit or stash them first."
    git status --short
    exit 1
fi

if ! git diff --cached --quiet; then
    log_error "You have staged changes. Please commit or unstage them first."
    git status --short
    exit 1
fi

# Read current version
if [[ ! -f "$VERSION_FILE" ]]; then
    log_error "VERSION file not found at $VERSION_FILE"
    exit 1
fi

CURRENT_VERSION=$(grep -E '^v?[0-9]+\.[0-9]+\.[0-9]+' "$VERSION_FILE" | head -1 | sed 's/^v//')
if [[ -z "$CURRENT_VERSION" ]]; then
    log_error "Could not parse version from $VERSION_FILE"
    exit 1
fi

log_info "Current version: v$CURRENT_VERSION"

# Parse version components
IFS='.' read -r Major Minor Patch <<< "$CURRENT_VERSION"
Major=${Major:-0}
Minor=${Minor:-0}
Patch=${Patch:-0}

# Bump version
case "$BUMP_TYPE" in
    patch)
        Patch=$((Patch + 1))
        ;;
    minor)
        Minor=$((Minor + 1))
        Patch=0
        ;;
    major)
        Major=$((Major + 1))
        Minor=0
        Patch=0
        ;;
esac

NEW_VERSION="v${Major}.${Minor}.${Patch}"
log_info "New version: $NEW_VERSION"

if $DRY_RUN; then
    log_warn "[DRY RUN] Would update $VERSION_FILE to $NEW_VERSION"
    log_warn "[DRY RUN] Would create git tag: $NEW_VERSION"
    log_warn "[DRY RUN] Would push tag to origin"
    exit 0
fi

# Update VERSION file
echo "$NEW_VERSION" > "$VERSION_FILE"
log_info "Updated $VERSION_FILE"

# Stage the version file
git add "$VERSION_FILE"

# Create annotated tag
git tag -a "$NEW_VERSION" -m "Release $NEW_VERSION"

log_info "Created git tag: $NEW_VERSION"

# Push tag to origin
log_info "Pushing tag to origin..."
git push origin "$NEW_VERSION"

echo ""
log_info "Done! Tag $NEW_VERSION has been pushed."
log_info "GitHub Actions will now build and release the binaries."
log_info "Watch the release at: https://github.com/looplj/axonhub/actions"
