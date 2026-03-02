#!/bin/bash

# AxonClaw Installer
# This script downloads and installs the latest AxonClaw release.
# It specifically targets the 'axonclaw' binary from the AxonHub release.

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
REPO="looplj/axonhub"
GITHUB_API="https://api.github.com/repos/${REPO}"
INSTALL_DIR="${INSTALL_DIR:-.}"
BINARY_NAME="axonclaw"

print_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

curl_gh() {
    # Curl helper for GitHub with proper headers and optional token
    local url="$1"
    local headers=(
        -H "Accept: application/vnd.github+json"
        -H "X-GitHub-Api-Version: 2022-11-28"
        -H "User-Agent: axonclaw-installer"
    )
    if [[ -n "$GITHUB_TOKEN" ]]; then
        headers+=( -H "Authorization: Bearer $GITHUB_TOKEN" )
    fi
    curl -fsSL "${headers[@]}" "$url"
}

detect_platform() {
    local os=""
    local arch=""

    case "$(uname -s)" in
        Linux*)     os="linux" ;;
        Darwin*)    os="darwin" ;;
        CYGWIN*|MINGW*|MSYS*) os="windows" ;;
        *)          os="linux" ;;
    esac

    case "$(uname -m)" in
        x86_64|amd64)   arch="amd64" ;;
        arm64|aarch64)  arch="arm64" ;;
        armv7l|armv6l)  arch="arm" ;;
        i386|i686)      arch="386" ;;
        *)              arch="amd64" ;;
    esac

    echo "${os}-${arch}"
}

get_latest_release() {
    print_info "Fetching latest release information..."
    
    local tag_name
    # Try GitHub API first
    if json=$(curl_gh "${GITHUB_API}/releases/latest" 2>/dev/null); then
        # Try to use jq if available, otherwise fallback to sed
        if command -v jq >/dev/null 2>&1; then
            tag_name=$(echo "$json" | jq -r .tag_name)
        else
            tag_name=$(echo "$json" | tr -d '\n\r\t' | sed -nE 's/.*"tag_name"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/p' | head -1)
        fi
    fi
    
    # Fallback: follow the HTML redirect to the latest tag
    if [[ -z "$tag_name" || "$tag_name" == "null" ]]; then
        print_warning "API failed or rate-limited, falling back to HTML redirect..."
        local final_url
        final_url=$(curl -fsSL -H "User-Agent: axonclaw-installer" -o /dev/null -w "%{url_effective}" "https://github.com/${REPO}/releases/latest" || true)
        tag_name=$(echo "$final_url" | sed -nE 's#.*/tag/([^/]+).*#\1#p' | head -1)
    fi
    
    if [[ -z "$tag_name" ]]; then
        print_error "Could not determine latest release version"
        exit 1
    fi
    
    echo "$tag_name"
}

get_asset_download_url() {
    local version=$1
    local platform=$2
    local os_name="${platform%%-*}"
    local arch="${platform##*-}"
    local url=""
    
    print_info "Resolving asset download URL for ${version} (${platform})..."
    
    # We need to find an asset that contains "axonclaw" and the platform
    # The release contains multiple binaries (axonhub, axonclaw), so we must filter carefully
    
    if json=$(curl_gh "${GITHUB_API}/releases/tags/${version}" 2>/dev/null); then
        if command -v jq >/dev/null 2>&1; then
            # Filter for assets containing 'axonclaw' and matching platform (os and arch)
            # We construct a regex to match the platform parts
            url=$(echo "$json" | jq -r --arg os "$os_name" --arg arch "$arch" '.assets[]?.browser_download_url | select(contains("axonclaw")) | select(contains($os)) | select(contains($arch))' | head -n1)
        else
            # Fallback using grep/sed
            # This is a bit hacky but works for standard JSON formatting
            url=$(echo "$json" \
                | grep "browser_download_url" \
                | grep "axonclaw" \
                | grep "$os_name" \
                | grep "$arch" \
                | head -1 \
                | sed -E 's/.*"([^"]+)".*/\1/')
        fi
    fi
    
    # Fallback to constructed URL if API failed or no asset matched
    if [[ -z "$url" ]]; then
        print_warning "API failed or no asset matched; trying constructed URL..."
        # Try tar.gz first
        local candidate="https://github.com/${REPO}/releases/download/${version}/axonclaw-${os_name}-${arch}.tar.gz"
        if curl -fsI "$candidate" >/dev/null 2>&1; then
            url="$candidate"
        else
            # Try zip
            candidate="https://github.com/${REPO}/releases/download/${version}/axonclaw-${os_name}-${arch}.zip"
            if curl -fsI "$candidate" >/dev/null 2>&1; then
                url="$candidate"
            fi
        fi
    fi
    
    if [[ -z "$url" ]]; then
        print_error "Could not find a matching asset for axonclaw on ${platform} in release ${version}"
        exit 1
    fi
    
    echo "$url"
}

download_and_extract() {
    local version="$1"
    local platform="$2"
    local download_url="$3"
    
    local temp_dir
    temp_dir=$(mktemp -d)
    local filename
    filename=$(basename "$download_url")
    local archive_path="${temp_dir}/${filename}"

    print_info "Downloading ${filename}..."
    print_info "URL: ${download_url}"

    if ! curl -fsSL -o "$archive_path" "$download_url"; then
        print_error "Failed to download binary"
        rm -rf "$temp_dir"
        exit 1
    fi

    print_success "Download completed"
    
    print_info "Extracting to ${INSTALL_DIR}..."
    mkdir -p "$INSTALL_DIR"

    if [[ "$filename" == *.zip ]]; then
        if ! command -v unzip >/dev/null 2>&1; then
            print_error "unzip command not found. Please install unzip."
            rm -rf "$temp_dir"
            exit 1
        fi
        if ! unzip -o -q "$archive_path" -d "$INSTALL_DIR"; then
            print_error "Failed to extract zip archive"
            rm -rf "$temp_dir"
            exit 1
        fi
    else
        # Assume tar.gz
        if ! tar -xzf "$archive_path" -C "$INSTALL_DIR"; then
            print_error "Failed to extract tar archive"
            rm -rf "$temp_dir"
            exit 1
        fi
    fi

    rm -rf "$temp_dir"
    print_success "Extraction completed"
}

make_scripts_executable() {
    local target_dir="$1"

    for script in start.sh stop.sh restart.sh; do
        if [[ -f "${target_dir}/${script}" ]]; then
            chmod +x "${target_dir}/${script}"
            print_info "Made ${script} executable"
        fi
    done

    if [[ -f "${target_dir}/${BINARY_NAME}" ]]; then
        chmod +x "${target_dir}/${BINARY_NAME}"
        print_info "Made ${BINARY_NAME} executable"
    fi
}

start_axonclaw() {
    local target_dir="$1"

    if [[ -f "${target_dir}/start.sh" ]]; then
        print_info "Starting axonclaw..."
        cd "$target_dir" && ./start.sh
    else
        print_warning "start.sh not found, axonclaw not started automatically"
        print_info "To start manually, run: cd ${target_dir} && ./start.sh"
    fi
}

main() {
    print_info "AxonClaw Installer"
    print_info "=================="
    print_info "Note: This installer targets the 'axonclaw' component from the AxonHub project."

    local platform
    platform=$(detect_platform)
    print_info "Detected platform: ${platform}"

    local version
    version=$(get_latest_release)
    print_info "Latest version: ${version}"

    local download_url
    download_url=$(get_asset_download_url "$version" "$platform")
    
    download_and_extract "$version" "$platform" "$download_url"

    make_scripts_executable "$INSTALL_DIR"

    print_success "AxonClaw ${version} installed successfully to ${INSTALL_DIR}"

    if [[ -n "$AXONCLAW_NAME" ]] || [[ -n "$AXONCLAW_BASE_URL" ]] || [[ -n "$AXONCLAW_API_KEY" ]]; then
        start_axonclaw "$INSTALL_DIR"
    else
        print_info "Environment variables not set, skipping auto-start"
        print_info "To start axonclaw manually:"
        print_info "  cd ${INSTALL_DIR} && ./start.sh"
        print_info ""
        print_info "Or with environment variables:"
        print_info "  AXONCLAW_NAME=my-agent AXONCLAW_BASE_URL=http://localhost:8090 AXONCLAW_API_KEY=your-key ./start.sh"
    fi
}

main "$@"
