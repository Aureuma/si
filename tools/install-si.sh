#!/usr/bin/env bash
set -euo pipefail

#
# si installer (macOS + Linux)
# - Builds from a local checkout when available, else clones and builds.
# - Ensures a compatible Go toolchain (downloads a user-local Go tarball if needed).
# - Installs to ~/.local/bin/si by default (or /usr/local/bin/si when run as root).
# - Builds a slim binary by default (trimpath, no VCS embedding, stripped symbols).
#

SCRIPT_NAME="install-si.sh"

usage() {
  cat <<'EOF'
Install the si CLI.

Usage:
  tools/install-si.sh
  tools/install-si.sh [flags]

Flags:
  --backend <local|sun> Backend intent for post-install guidance (default: local)
  --source-dir <path>     Build from an existing si repo checkout (auto-detected if omitted)
  --repo <owner/repo>     GitHub repo to clone when not building locally (default: Aureuma/si)
  --repo-url <url>        Full git URL to clone (overrides --repo)
  --ref <ref>             Git ref to clone/checkout (default: main)
  --version <tag|latest>  Convenience: sets --ref to a tag (or resolves latest GitHub release tag)

  --install-dir <dir>     Install directory (default: ~/.local/bin, or /usr/local/bin when root)
  --install-path <path>   Install full path (overrides --install-dir)
  --force                 Overwrite an existing install
  --uninstall             Remove the installed binary and exit

  --go-mode <auto|system> auto: download Go if missing/outdated (default)
                          system: require a compatible go in PATH
  --go-version <ver>      Override required Go version (default: parsed from tools/si/go.mod)

  --build-tags <tags>     Go build tags (comma-separated)
  --build-ldflags <flags> Go linker flags (default: "-s -w")

  --link-go               Force: symlink go/gofmt into the install dir (even if a system Go exists)
  --no-link-go            Disable: do not symlink go/gofmt even if Go is auto-downloaded
  --with-buildx           Force: if docker buildx is missing, install it as a user-level Docker CLI plugin
  --no-buildx             Disable: do not attempt buildx installation (warn only)

  --os <linux|darwin>     Override OS detection (primarily for dry-run/testing)
  --arch <amd64|arm64>    Override arch detection (primarily for dry-run/testing)

  --tmp-dir <dir>         Temporary working directory (default: mktemp)
  -y, --yes               Assume yes for interactive prompts
  --dry-run               Print actions without changing anything
  --quiet                 Reduce output
  --no-path-hint          Skip PATH guidance after install
  -h, --help              Show this help

Notes:
  - With no flags, this script automatically picks the next-best option:
      1) Use a local checkout if present.
      2) Otherwise fetch the repo (git clone preferred; GitHub tarball fallback if git is missing).
      3) Ensure a compatible Go toolchain (auto-download if needed).
      4) Install si to the first writable default install dir.
  - This script does not modify your shell rc files. If your install dir isn't on PATH,
    it prints the exact line to add for bash/zsh.
  - Backend selection is configuration guidance only:
      - local (default): keep secrets/profile state local-first.
      - sun: installer prints follow-up `si sun auth login` steps.
    The installer intentionally does not collect or store Sun tokens.
  - Advanced CI checks for installer settings mutation are available via:
      tools/install-si-settings.sh --settings <path> --default-browser <safari|chrome> --check
      tools/install-si-settings.sh --settings <path> --default-browser <safari|chrome> --print
  - For si vault on Linux, installing "secret-tool" enables keyring storage:
      Ubuntu/Debian: sudo apt install -y libsecret-tools
EOF
}

log() {
  local level="$1"; shift
  if [[ "${QUIET}" -eq 1 && "${level}" == "info" ]]; then
    return 0
  fi
  case "${level}" in
    info) printf '%s\n' "$*" ;;
    warn) printf '%s\n' "⚠️  $*" >&2 ;;
    err)  printf '%s\n' "❌ $*" >&2 ;;
    *)    printf '%s\n' "$*" ;;
  esac
}

die() {
  log err "$*"
  exit 1
}

need_cmd() {
  local c="$1"
  command -v "${c}" >/dev/null 2>&1 || die "missing required command: ${c}"
}

need_downloader() {
  if command -v curl >/dev/null 2>&1; then
    return 0
  fi
  if command -v wget >/dev/null 2>&1; then
    return 0
  fi
  die "missing required command: curl or wget"
}

http_get() {
  local url="$1"
  local header="${2:-}"
  need_downloader
  if command -v curl >/dev/null 2>&1; then
    if [[ -n "${header}" ]]; then
      curl --proto "=https" --tlsv1.2 -fsSL -H "${header}" "${url}"
    else
      curl --proto "=https" --tlsv1.2 -fsSL "${url}"
    fi
    return 0
  fi
  if [[ -n "${header}" ]]; then
    wget -q -O - --header "${header}" "${url}"
  else
    wget -q -O - "${url}"
  fi
}

http_get_to_file() {
  local url="$1"
  local out="$2"
  local header="${3:-}"
  need_downloader
  if command -v curl >/dev/null 2>&1; then
    if [[ -n "${header}" ]]; then
      curl --proto "=https" --tlsv1.2 -fsSL -H "${header}" -o "${out}" "${url}"
    else
      curl --proto "=https" --tlsv1.2 -fsSL -o "${out}" "${url}"
    fi
    return 0
  fi
  if [[ -n "${header}" ]]; then
    wget -q -O "${out}" --header "${header}" "${url}"
  else
    wget -q -O "${out}" "${url}"
  fi
}

mktemp_dir() {
  # mktemp differs between GNU (Linux) and BSD (macOS). Prefer a portable shim.
  local d=""
  if d="$(mktemp -d 2>/dev/null)"; then
    printf '%s' "${d}"
    return 0
  fi
  if d="$(mktemp -d -t si 2>/dev/null)"; then
    printf '%s' "${d}"
    return 0
  fi
  die "mktemp -d failed"
}

is_tty() {
  [[ -t 1 ]]
}

confirm_yes_no() {
  local prompt="$1"
  local answer=""
  if [[ "${ASSUME_YES}" -eq 1 ]]; then
    return 0
  fi
  if ! is_tty; then
    return 1
  fi
  # shellcheck disable=SC2162
  read -r -p "${prompt} [y/N] " answer
  answer="$(echo "${answer}" | tr '[:upper:]' '[:lower:]')"
  case "${answer}" in
    y|yes) return 0 ;;
    *) return 1 ;;
  esac
}

trim() {
  # shellcheck disable=SC2001
  echo "$1" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//'
}

version_ge() {
  # Return 0 if $1 >= $2
  local a="$1"
  local b="$2"
  local IFS=.
  local -a av=(${a})
  local -a bv=(${b})
  local i
  for i in 0 1 2; do
    local ai="${av[i]:-0}"
    local bi="${bv[i]:-0}"
    # Avoid octal interpretation.
    if ((10#${ai} > 10#${bi})); then
      return 0
    fi
    if ((10#${ai} < 10#${bi})); then
      return 1
    fi
  done
  return 0
}

sha256_file() {
  local path="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${path}" | awk '{print $1}'
    return 0
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "${path}" | awk '{print $1}'
    return 0
  fi
  die "missing sha256 tool (need sha256sum or shasum)"
}

sha256_file_best_effort() {
  local path="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${path}" | awk '{print $1}'
    return 0
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "${path}" | awk '{print $1}'
    return 0
  fi
  return 1
}

run() {
  if [[ "${DRY_RUN}" -eq 1 ]]; then
    log info "+ $*"
    return 0
  fi
  "$@"
}

resolve_latest_release_tag() {
  local owner_repo="$1"
  # For private repos, users can export GH_TOKEN/GITHUB_TOKEN.
  local token=""
  if [[ -n "${GH_TOKEN:-}" ]]; then token="${GH_TOKEN}"; fi
  if [[ -n "${GITHUB_TOKEN:-}" ]]; then token="${GITHUB_TOKEN}"; fi

  local url="https://api.github.com/repos/${owner_repo}/releases/latest"
  local json
  local header=""
  if [[ -n "${token}" ]]; then
    header="Authorization: Bearer ${token}"
  fi
  if ! json="$(http_get "${url}" "${header}" 2>/dev/null)"; then
    return 1
  fi
  # Extract "tag_name": "vX.Y.Z"
  local tag
  tag="$(printf '%s' "${json}" | tr -d '\r' | grep -E '"tag_name"[[:space:]]*:' | head -n 1 | sed -E 's/.*"tag_name"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/')"
  tag="$(trim "${tag}")"
  [[ -n "${tag}" ]] || return 1
  printf '%s' "${tag}"
}

detect_os() {
  if [[ -n "${OS_OVERRIDE}" ]]; then
    echo "${OS_OVERRIDE}"
    return 0
  fi
  local s
  s="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "${s}" in
    linux) echo "linux" ;;
    darwin) echo "darwin" ;;
    *) die "unsupported OS: $(uname -s)" ;;
  esac
}

detect_arch() {
  if [[ -n "${ARCH_OVERRIDE}" ]]; then
    echo "${ARCH_OVERRIDE}"
    return 0
  fi
  local m
  m="$(uname -m | tr '[:upper:]' '[:lower:]')"
  case "${m}" in
    x86_64|amd64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *) die "unsupported architecture: $(uname -m)" ;;
  esac
}

normalize_os_arch_overrides() {
  OS_OVERRIDE="$(trim "${OS_OVERRIDE}")"
  ARCH_OVERRIDE="$(trim "${ARCH_OVERRIDE}")"
  if [[ -n "${OS_OVERRIDE}" ]]; then
    OS_OVERRIDE="$(echo "${OS_OVERRIDE}" | tr '[:upper:]' '[:lower:]')"
    case "${OS_OVERRIDE}" in
      linux) ;;
      darwin|mac|macos) OS_OVERRIDE="darwin" ;;
      *) die "invalid --os ${OS_OVERRIDE} (expected linux or darwin)" ;;
    esac
  fi
  if [[ -n "${ARCH_OVERRIDE}" ]]; then
    ARCH_OVERRIDE="$(echo "${ARCH_OVERRIDE}" | tr '[:upper:]' '[:lower:]')"
    case "${ARCH_OVERRIDE}" in
      amd64|x86_64) ARCH_OVERRIDE="amd64" ;;
      arm64|aarch64) ARCH_OVERRIDE="arm64" ;;
      *) die "invalid --arch ${ARCH_OVERRIDE} (expected amd64 or arm64)" ;;
    esac
  fi
}

repo_root_from_cwd() {
  local root=""
  if root="$(git rev-parse --show-toplevel 2>/dev/null)"; then
    if [[ -f "${root}/tools/si/go.mod" ]]; then
      printf '%s' "${root}"
      return 0
    fi
  fi
  return 1
}

repo_root_from_script() {
  local source="${BASH_SOURCE[0]:-}"
  [[ -n "${source}" && -e "${source}" ]] || return 1
  while [[ -h "${source}" ]]; do
    local dir
    dir="$(cd -P "$(dirname "${source}")" && pwd)"
    source="$(readlink "${source}")"
    [[ "${source}" != /* ]] && source="${dir}/${source}"
  done
  local root
  root="$(cd "$(dirname "${source}")/.." && pwd)"
  [[ -f "${root}/tools/si/go.mod" ]] || return 1
  printf '%s' "${root}"
}

parse_go_mod_required_version() {
  local root="$1"
  local gomod="${root}/tools/si/go.mod"
  [[ -f "${gomod}" ]] || die "go.mod not found at ${gomod}"
  # Prefer the go.mod toolchain directive when present, else fall back to the "go" directive.
  # Examples:
  #   go 1.25.0
  #   toolchain go1.25.7
  local tc
  tc="$(grep -E '^toolchain[[:space:]]+go[0-9]+\.[0-9]+\.[0-9]+' "${gomod}" | head -n 1 | awk '{print $2}')"
  tc="$(trim "${tc}")"
  if [[ -n "${tc}" ]]; then
    tc="${tc#go}"
    tc="$(trim "${tc}")"
    [[ -n "${tc}" ]] || die "failed to parse toolchain Go version from ${gomod}"
    printf '%s' "${tc}"
    return 0
  fi

  local ver
  ver="$(grep -E '^go[[:space:]]+[0-9]+\.[0-9]+(\.[0-9]+)?' "${gomod}" | head -n 1 | awk '{print $2}')"
  ver="$(trim "${ver}")"
  [[ -n "${ver}" ]] || die "failed to parse required Go version from ${gomod}"
  printf '%s' "${ver}"
}

go_version_from_bin() {
  local go_bin="$1"
  local out
  out="$("${go_bin}" version 2>/dev/null || true)"
  # Example: "go version go1.25.0 linux/amd64"
  local tok
  tok="$(printf '%s' "${out}" | awk '{print $3}' | head -n 1)"
  tok="$(trim "${tok}")"
  tok="${tok#go}"
  [[ -n "${tok}" ]] || return 1
  printf '%s' "${tok}"
}

ensure_go() {
  local required="$1"
  local os="$2"
  local arch="$3"

  local go_bin=""
  if command -v go >/dev/null 2>&1; then
    go_bin="$(command -v go)"
    local have
    have="$(go_version_from_bin "${go_bin}" || true)"
    if [[ -n "${have}" ]] && version_ge "${have}" "${required}"; then
      log info "go: using system go ${have} (${go_bin})" >&2
      GO_BIN_KIND="system"
      printf '%s' "${go_bin}"
      return 0
    fi
    log warn "go: system go is missing or too old (have ${have:-unknown}, need ${required})" >&2
  fi

  if [[ "${GO_MODE}" == "system" ]]; then
    die "Go ${required}+ is required in PATH (go.mod requires ${required}); install Go or use --go-mode auto"
  fi

  local home="${HOME:-}"
  [[ -n "${home}" ]] || die "HOME is not set"

  local base="${home}/.local/share/si/go"
  local dest="${base}/go${required}"
  local go_path="${dest}/bin/go"

  if [[ -x "${go_path}" ]]; then
    local have
    have="$(go_version_from_bin "${go_path}" || true)"
    if [[ -n "${have}" ]] && version_ge "${have}" "${required}"; then
      log info "go: using cached go ${have} (${go_path})" >&2
      GO_BIN_KIND="cached"
      printf '%s' "${go_path}"
      return 0
    fi
    log warn "go: cached go exists but version mismatch (have ${have:-unknown}, need ${required}); reinstalling" >&2
  fi

  need_downloader
  need_cmd tar

  local tgz="go${required}.${os}-${arch}.tar.gz"
  local url="https://dl.google.com/go/${tgz}"
  local sha_url="${url}.sha256"

  log info "go: downloading ${url}" >&2
  if [[ "${DRY_RUN}" -eq 1 ]]; then
    log info "go: would verify sha256 via ${sha_url}" >&2
    log info "go: would install to ${dest}" >&2
    GO_BIN_KIND="downloaded"
    printf '%s' "${go_path}"
    return 0
  fi

  local work
  work="$(mktemp_dir)"
  # Best-effort cleanup happens at the end of this function; on failure we may
  # leave the directory behind to preserve diagnostics.

  local tgz_path="${work}/${tgz}"
  http_get_to_file "${url}" "${tgz_path}"

  local expected
  expected="$(http_get "${sha_url}" | awk '{print $1}' | tr -d '\r\n')"
  expected="$(trim "${expected}")"
  [[ -n "${expected}" ]] || die "go: failed to fetch expected sha256"

  local actual
  actual="$(sha256_file "${tgz_path}")"
  if [[ "${actual}" != "${expected}" ]]; then
    die "go: sha256 mismatch for ${tgz} (expected ${expected}, got ${actual})"
  fi

  mkdir -p "${base}"
  rm -rf "${dest}"
  mkdir -p "${work}/extract"
  tar -C "${work}/extract" -xzf "${tgz_path}"
  [[ -d "${work}/extract/go" ]] || die "go: expected go/ directory after extracting ${tgz}"
  mv "${work}/extract/go" "${dest}"
  [[ -x "${go_path}" ]] || die "go: installed go binary not found at ${go_path}"

  local have
  have="$(go_version_from_bin "${go_path}" || true)"
  if [[ -z "${have}" ]] || ! version_ge "${have}" "${required}"; then
    die "go: installed go version check failed (have ${have:-unknown}, need ${required})"
  fi

  log info "✅ go: installed ${have} at ${go_path}" >&2
  rm -rf "${work}" || true
  GO_BIN_KIND="downloaded"
  printf '%s' "${go_path}"
}

clone_repo() {
  local repo_url="$1"
  local ref="$2"
  local out_dir="$3"

  need_cmd git
  local -a git_cmd=(git)
  if [[ -d "${repo_url}" ]]; then
    # Local checkouts mounted into containers may trigger Git's safe.directory checks.
    git_cmd+=( -c "safe.directory=${repo_url}" )
  elif [[ "${repo_url}" =~ ^file:// ]]; then
    local repo_path="${repo_url#file://}"
    if [[ -d "${repo_path}" ]]; then
      git_cmd+=( -c "safe.directory=${repo_path}" )
    fi
  fi
  if [[ "${DRY_RUN}" -eq 1 ]]; then
    log info "git: would clone ${repo_url} to ${out_dir} (ref ${ref})"
    return 0
  fi

  # If ref looks like a commit SHA, do a normal clone then checkout.
  if [[ "${ref}" =~ ^[0-9a-fA-F]{7,40}$ ]]; then
    "${git_cmd[@]}" clone "${repo_url}" "${out_dir}"
    "${git_cmd[@]}" -C "${out_dir}" checkout "${ref}"
    return 0
  fi
  "${git_cmd[@]}" clone --depth 1 --branch "${ref}" "${repo_url}" "${out_dir}"
}

ensure_build_prereqs() {
  local -a required
  required=(awk chmod cp find grep head id mkdir mv sed tr uname)
  local missing=()
  local c
  for c in "${required[@]}"; do
    if ! command -v "${c}" >/dev/null 2>&1; then
      missing+=("${c}")
    fi
  done
  if [[ "${#missing[@]}" -gt 0 ]]; then
    die "missing required commands for installer: ${missing[*]}"
  fi
}

build_si() {
  local root="$1"
  local go_bin="$2"
  local out_bin="$3"

  [[ -f "${root}/tools/si/go.mod" ]] || die "not a si repo root: ${root} (missing tools/si/go.mod)"
  if [[ "${DRY_RUN}" -eq 1 ]]; then
    log info "🛠️  build: would run (${root}): ${go_bin} build -trimpath -buildvcs=false -ldflags \"${BUILD_LDFLAGS}\" ${BUILD_TAGS:+-tags \"${BUILD_TAGS}\"} -o ${out_bin} ./tools/si"
    return 0
  fi
  (
    cd "${root}"
    # Keep the default build artifact small and reproducible:
    # -trimpath: avoid embedding local paths
    # -buildvcs=false: avoid embedding git metadata (also avoids requiring git at build time)
    local -a args
    args=(build -trimpath -buildvcs=false)
    if [[ -n "${BUILD_TAGS}" ]]; then
      args+=(-tags "${BUILD_TAGS}")
    fi
    args+=(-ldflags "${BUILD_LDFLAGS}" -o "${out_bin}" ./tools/si)
    "${go_bin}" "${args[@]}"
  )
}

install_bin() {
  local src="$1"
  local dst="$2"
  local dst_dir
  dst_dir="$(dirname "${dst}")"

  if [[ "${DRY_RUN}" -eq 1 ]]; then
    log info "📦 install: would install ${src} -> ${dst}"
    return 0
  fi

  mkdir -p "${dst_dir}"
  if [[ -e "${dst}" && "${FORCE}" -ne 1 ]]; then
    die "install path exists: ${dst} (re-run with --force to overwrite)"
  fi
  if [[ ! -w "${dst_dir}" ]]; then
    die "install dir is not writable: ${dst_dir} (choose --install-dir or run as a user with write access)"
  fi

  local tmp="${dst_dir}/.si.tmp.$$"
  cp "${src}" "${tmp}"
  chmod 0755 "${tmp}"
  mv -f "${tmp}" "${dst}"
}

install_symlink() {
  local target="$1"
  local linkpath="$2"
  local linkdir
  linkdir="$(dirname "${linkpath}")"

  if [[ "${DRY_RUN}" -eq 1 ]]; then
    log info "🔗 install: would symlink ${linkpath} -> ${target}"
    return 0
  fi

  mkdir -p "${linkdir}"
  if [[ -e "${linkpath}" && "${FORCE}" -ne 1 ]]; then
    log warn "install: not overwriting existing path (use --force): ${linkpath}"
    return 0
  fi
  if [[ ! -w "${linkdir}" ]]; then
    log warn "install: cannot write symlink into: ${linkdir}"
    return 0
  fi
  ln -sfn "${target}" "${linkpath}"
}

prompt_default_browser() {
  if [[ "${ASSUME_YES}" -eq 1 ]]; then
    return 1
  fi
  if ! is_tty; then
    return 1
  fi
  local default_choice="chrome"
  if [[ "${OS}" == "darwin" ]]; then
    default_choice="safari"
  fi
  local answer=""
  log info ""
  log info "Browser profile preference for si login:"
  log info "  1) Safari"
  log info "  2) Chrome"
  log info "  3) Skip"
  # shellcheck disable=SC2162
  read -r -p "Select default browser [1-3] (default: ${default_choice}): " answer
  answer="$(trim "${answer}")"
  if [[ -z "${answer}" ]]; then
    answer="${default_choice}"
  fi
  answer="$(echo "${answer}" | tr '[:upper:]' '[:lower:]')"
  case "${answer}" in
    1|safari)
      printf '%s' "safari"
      return 0
      ;;
    2|chrome)
      printf '%s' "chrome"
      return 0
      ;;
    3|skip|none|no)
      return 1
      ;;
    *)
      log warn "invalid choice: ${answer}; skipping browser configuration"
      return 1
      ;;
  esac
}

write_default_browser_setting() {
  local browser="$1"
  browser="$(echo "$(trim "${browser}")" | tr '[:upper:]' '[:lower:]')"
  case "${browser}" in
    safari|chrome) ;;
    *) return 1 ;;
  esac
  [[ -n "${HOME:-}" ]] || return 1
  local settings_path="${HOME}/.si/settings.toml"
  local script_dir
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  local helper="${script_dir}/install-si-settings.sh"
  if [[ -x "${helper}" ]]; then
    "${helper}" --settings "${settings_path}" --default-browser "${browser}"
    return $?
  fi
  return 1
}

configure_default_browser_setting() {
  if [[ "${DRY_RUN}" -eq 1 ]]; then
    return 0
  fi
  local browser=""
  if browser="$(prompt_default_browser)"; then
    if write_default_browser_setting "${browser}"; then
      log info "✅ configured codex.login.default_browser=${browser} in ~/.si/settings.toml"
      return 0
    fi
    log warn "failed to configure default browser setting"
  fi
}

ensure_docker_buildx() {
  # If docker is present but buildx isn't, try a next-best fix:
  # - Auto mode: attempt install only in an interactive TTY (avoid surprising CI/tests).
  # - Force mode: always attempt.
  # - Never mode: warn only.
  if ! command -v docker >/dev/null 2>&1; then
    return 0
  fi
  if docker buildx version >/dev/null 2>&1; then
    return 0
  fi

  if [[ "${BUILDX_MODE}" == "never" ]]; then
    log warn "docker buildx is not available (BuildKit features like build secrets may be disabled)"
    log info "🔧 To install buildx on Debian/Ubuntu (Docker repo): sudo apt-get install -y docker-buildx-plugin"
    return 0
  fi
  if [[ "${BUILDX_MODE}" == "auto" ]] && ! is_tty; then
    log warn "docker buildx is not available (BuildKit features like build secrets may be disabled)"
    log info "💡 Tip: re-run in a TTY to auto-install buildx, or pass --with-buildx."
    return 0
  fi
  if [[ "${BUILDX_MODE}" == "auto" && "${DRY_RUN}" -eq 0 ]]; then
    if ! confirm_yes_no "docker buildx is missing. Install a user-level plugin now?"; then
      log warn "docker buildx: skipped installation (continue without buildx)"
      return 0
    fi
  fi

  local os="$1"
  local arch="$2"
  local docker_config="${DOCKER_CONFIG:-${HOME:-}/.docker}"
  if [[ -z "${docker_config}" ]]; then
    log warn "docker buildx: DOCKER_CONFIG/HOME not set; cannot install buildx plugin"
    return 0
  fi

  local plugin_dir="${docker_config%/}/cli-plugins"
  local plugin_path="${plugin_dir}/docker-buildx"

  need_cmd chmod
  need_downloader
  local tag
  log info "🔧 docker buildx: resolving latest release..."
  if ! tag="$(resolve_latest_release_tag "docker/buildx" 2>/dev/null)"; then
    log warn "docker buildx: failed to resolve latest release tag"
    return 0
  fi

  local asset="buildx-${tag}.${os}-${arch}"
  local url="https://github.com/docker/buildx/releases/download/${tag}/${asset}"
  local checksums_url="https://github.com/docker/buildx/releases/download/${tag}/checksums.txt"

  log info "⬇️  docker buildx: downloading ${url}"
  if [[ "${DRY_RUN}" -eq 1 ]]; then
    log info "docker buildx: would verify sha256 via ${checksums_url}"
    log info "docker buildx: would install to ${plugin_path}"
    return 0
  fi

  local work
  work="$(mktemp_dir)"
  local bin="${work}/${asset}"
  if ! http_get_to_file "${url}" "${bin}" 2>/dev/null; then
    log warn "docker buildx: download failed: ${url}"
    rm -rf "${work}" || true
    return 0
  fi

  # Best-effort checksum verification.
  if sums="$(http_get "${checksums_url}" 2>/dev/null)"; then
    expected="$(printf '%s\n' "${sums}" | awk -v f="${asset}" '$2==f {print $1}' | head -n 1 | tr -d '\r\n')"
    expected="$(trim "${expected}")"
    if [[ -n "${expected}" ]]; then
      if actual="$(sha256_file_best_effort "${bin}" 2>/dev/null)"; then
        if [[ "${actual}" != "${expected}" ]]; then
          log warn "docker buildx: sha256 mismatch for ${asset} (expected ${expected}, got ${actual}); not installing"
          rm -rf "${work}" || true
          return 0
        fi
      else
        log warn "docker buildx: sha256 tool not found; skipping sha256 verification"
      fi
    else
      log warn "docker buildx: checksums.txt did not contain an entry for ${asset}; skipping sha256 verification"
    fi
  else
    log warn "docker buildx: failed to fetch checksums.txt; skipping sha256 verification"
  fi

  mkdir -p "${plugin_dir}"
  cp "${bin}" "${plugin_path}"
  chmod 0755 "${plugin_path}"
  rm -rf "${work}" || true

  if docker buildx version >/dev/null 2>&1; then
    log info "✅ docker buildx: installed plugin at ${plugin_path}"
  else
    log warn "docker buildx: installed, but docker still does not see buildx (check DOCKER_CONFIG and Docker version)"
  fi
}

post_install() {
  local dst="$1"
  local install_dir
  install_dir="$(dirname "${dst}")"

  if [[ "${DRY_RUN}" -eq 1 ]]; then
    return 0
  fi

  if ! "${dst}" version >/dev/null 2>&1; then
    die "installed si binary does not run: ${dst}"
  fi

  log info ""
  log info "✅ installed: ${dst}"
  log info "🔎 verify:    ${dst} --help"
  log info ""

  if [[ "${NO_PATH_HINT}" -eq 0 ]]; then
    case ":${PATH}:" in
      *":${install_dir}:"*) ;;
      *)
        if [[ "${install_dir}" == "${HOME:-}/.local/bin" ]]; then
          log warn "🧭 ~/.local/bin is not on PATH for this shell."
          log info "Add this to your shell rc (~/.bashrc or ~/.zshrc):"
          log info "  export PATH=\"${HOME}/.local/bin:\$PATH\""
          log info ""
        else
          log warn "🧭 install dir is not on PATH for this shell: ${install_dir}"
        fi
        ;;
    esac
  fi

  log info "🔐 First-time vault notes:"
  log info "  - Linux keyring support (recommended): sudo apt install -y libsecret-tools"
  log info "🗄️  Backend:"
  if [[ "${BACKEND_MODE}" == "sun" ]]; then
    log info "  - selected: sun (cloud sync)"
    log info "  - next: si sun auth login --url <sun-url> --token <sun-token> --account <slug> --auto-sync"
    log info "  - then: si sun profile push && si sun vault backup push"
  else
    log info "  - selected: local (default)"
    log info "  - optional cloud sync later: si sun auth login --url <sun-url> --token <sun-token> --account <slug> --auto-sync"
  fi
  log info "🏗️  Build notes:"
  log info "  - 'si build self' needs a Go toolchain. If this installer downloads Go, it will place a go shim next to si."
  log info "  - 'si build image' is best with Docker BuildKit/buildx enabled"
  log info "  - If you see legacy-builder warnings, check/unset: DOCKER_BUILDKIT=0"
  log info "  - In a host repo: git submodule update --init --recursive"
  log info "  - Then: si vault status --env dev"
}

QUIET=0
DRY_RUN=0
FORCE=0
UNINSTALL=0
NO_PATH_HINT=0
ASSUME_YES=0
LINK_GO_MODE="auto"
BUILDX_MODE="auto"
BACKEND_MODE="${SI_INSTALL_BACKEND:-local}"

REPO="Aureuma/si"
REPO_URL=""
REF="main"
VERSION=""
SOURCE_DIR=""
INSTALL_DIR=""
INSTALL_PATH=""
GO_MODE="auto"
GO_VERSION=""
BUILD_TAGS=""
BUILD_LDFLAGS="-s -w"
OS_OVERRIDE=""
ARCH_OVERRIDE=""
TMP_DIR=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help) usage; exit 0 ;;
    -y|--yes) ASSUME_YES=1; shift ;;
    --backend) BACKEND_MODE="$2"; shift 2 ;;
    --source-dir) SOURCE_DIR="$2"; shift 2 ;;
    --repo) REPO="$2"; shift 2 ;;
    --repo-url) REPO_URL="$2"; shift 2 ;;
    --ref) REF="$2"; shift 2 ;;
    --version) VERSION="$2"; shift 2 ;;
    --install-dir) INSTALL_DIR="$2"; shift 2 ;;
    --install-path) INSTALL_PATH="$2"; shift 2 ;;
    --force) FORCE=1; shift ;;
    --uninstall) UNINSTALL=1; shift ;;
    --go-mode) GO_MODE="$2"; shift 2 ;;
    --go-version) GO_VERSION="$2"; shift 2 ;;
    --build-tags) BUILD_TAGS="$2"; shift 2 ;;
    --build-ldflags) BUILD_LDFLAGS="$2"; shift 2 ;;
    --link-go) LINK_GO_MODE="always"; shift ;;
    --no-link-go) LINK_GO_MODE="never"; shift ;;
    --with-buildx) BUILDX_MODE="always"; shift ;;
    --no-buildx) BUILDX_MODE="never"; shift ;;
    --os) OS_OVERRIDE="$2"; shift 2 ;;
    --arch) ARCH_OVERRIDE="$2"; shift 2 ;;
    --tmp-dir) TMP_DIR="$2"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    --quiet) QUIET=1; shift ;;
    --no-path-hint) NO_PATH_HINT=1; shift ;;
    *)
      die "unknown argument: $1 (use --help)"
      ;;
  esac
done

normalize_os_arch_overrides

LINK_GO_MODE="$(echo "$(trim "${LINK_GO_MODE}")" | tr '[:upper:]' '[:lower:]')"
case "${LINK_GO_MODE}" in
  auto|always|never) ;;
  *) die "invalid link-go mode (expected auto|always|never)" ;;
esac

BUILDX_MODE="$(echo "$(trim "${BUILDX_MODE}")" | tr '[:upper:]' '[:lower:]')"
case "${BUILDX_MODE}" in
  auto|always|never) ;;
  *) die "invalid buildx mode (expected auto|always|never)" ;;
esac

BACKEND_MODE="$(echo "$(trim "${BACKEND_MODE}")" | tr '[:upper:]' '[:lower:]')"
case "${BACKEND_MODE}" in
  local|sun) ;;
  *) die "invalid --backend ${BACKEND_MODE} (expected local or sun)" ;;
esac

if [[ "${DRY_RUN}" -eq 0 ]]; then
  if [[ -n "${OS_OVERRIDE}" || -n "${ARCH_OVERRIDE}" ]]; then
    die "--os/--arch overrides are only supported with --dry-run (they do not make sense for a real install)"
  fi
fi

if [[ -n "${INSTALL_DIR}" && -n "${INSTALL_PATH}" ]]; then
  die "--install-dir and --install-path are mutually exclusive; pass only one"
fi

GO_MODE="$(echo "$(trim "${GO_MODE}")" | tr '[:upper:]' '[:lower:]')"
case "${GO_MODE}" in
  auto|system) ;;
  *) die "invalid --go-mode ${GO_MODE} (expected auto or system)" ;;
esac

if [[ -n "${VERSION}" ]]; then
  if [[ "${VERSION}" == "latest" ]]; then
    if [[ -z "${REPO_URL}" ]]; then
      log info "resolving latest release for ${REPO}..."
      if tag="$(resolve_latest_release_tag "${REPO}" 2>/dev/null)"; then
        REF="${tag}"
        log info "latest release tag: ${REF}"
      else
        log warn "failed to resolve latest release tag; falling back to --ref ${REF}"
      fi
    else
      log warn "--version latest requires --repo (owner/repo); ignoring and using --ref ${REF}"
    fi
  else
    REF="${VERSION}"
  fi
fi

if [[ -z "${INSTALL_PATH}" ]]; then
  if [[ -z "${INSTALL_DIR}" ]]; then
    if [[ "$(id -u)" -eq 0 ]]; then
      INSTALL_DIR="/usr/local/bin"
    else
      [[ -n "${HOME:-}" ]] || die "HOME is not set; pass --install-dir or --install-path"
      # Pick the first writable default install dir.
      for d in "${HOME}/.local/bin" "${HOME}/bin"; do
        if mkdir -p "${d}" 2>/dev/null && [[ -w "${d}" ]]; then
          INSTALL_DIR="${d}"
          break
        fi
      done
      [[ -n "${INSTALL_DIR}" ]] || die "no writable default install dir found under HOME; pass --install-dir or --install-path"
    fi
  fi
  INSTALL_PATH="${INSTALL_DIR%/}/si"
fi

  if [[ "${UNINSTALL}" -eq 1 ]]; then
    if [[ "${DRY_RUN}" -eq 1 ]]; then
      log info "🧹 uninstall: would remove ${INSTALL_PATH}"
      exit 0
    fi
    if [[ -e "${INSTALL_PATH}" ]]; then
      rm -f "${INSTALL_PATH}"
      log info "🧹 removed: ${INSTALL_PATH}"
    else
      log info "not installed: ${INSTALL_PATH}"
    fi
    exit 0
  fi

OS="$(detect_os)"
ARCH="$(detect_arch)"

if [[ -z "${SOURCE_DIR}" ]]; then
  if root="$(repo_root_from_cwd 2>/dev/null)"; then
    SOURCE_DIR="${root}"
  elif root="$(repo_root_from_script 2>/dev/null)"; then
    SOURCE_DIR="${root}"
  fi
fi

if [[ -n "${SOURCE_DIR}" ]]; then
  [[ -d "${SOURCE_DIR}" ]] || die "--source-dir does not exist: ${SOURCE_DIR}"
  [[ -f "${SOURCE_DIR}/tools/si/go.mod" ]] || die "--source-dir is not an si repo root (missing tools/si/go.mod): ${SOURCE_DIR}"
fi

ensure_build_prereqs

WORKDIR=""
cleanup() {
  if [[ -n "${WORKDIR}" && "${DRY_RUN}" -eq 0 ]]; then
    rm -rf "${WORKDIR}" || true
  fi
}
trap cleanup EXIT

if [[ -z "${SOURCE_DIR}" ]]; then
  if [[ -z "${TMP_DIR}" ]]; then
    WORKDIR="$(mktemp_dir)"
  else
    WORKDIR="${TMP_DIR%/}/si-install.$$"
    run mkdir -p "${WORKDIR}"
  fi
  local_dir="${WORKDIR}/src"
  if [[ -z "${REPO_URL}" ]]; then
    if [[ "${REPO}" == *"://"* || "${REPO}" == git@*:* ]]; then
      REPO_URL="${REPO}"
    else
      REPO_URL="https://github.com/${REPO}.git"
    fi
  fi
  fetch_github_tarball() {
    local repo_url="$1"
    local ref="$2"
    local out_dir="$3"

    if [[ ! "${repo_url}" =~ ^https://github.com/([^/]+/[^/.]+)(\\.git)?$ ]]; then
      return 1
    fi
    local owner_repo="${BASH_REMATCH[1]}"
    local repo_name="${owner_repo#*/}"

    # For private repos, users can export GH_TOKEN/GITHUB_TOKEN.
    local token=""
    if [[ -n "${GH_TOKEN:-}" ]]; then token="${GH_TOKEN}"; fi
    if [[ -n "${GITHUB_TOKEN:-}" ]]; then token="${GITHUB_TOKEN}"; fi
    local header=""
    if [[ -n "${token}" ]]; then
      header="Authorization: Bearer ${token}"
    fi

    need_downloader
    need_cmd tar
    local archive_url="https://github.com/${owner_repo}/archive/${ref}.tar.gz"

    log info "⬇️  repo: downloading ${archive_url}"
    if [[ "${DRY_RUN}" -eq 1 ]]; then
      log info "repo: would extract to ${out_dir}"
      return 0
    fi

    mkdir -p "${out_dir}"
    local tgz="${WORKDIR}/src.tgz"
    http_get_to_file "${archive_url}" "${tgz}" "${header}"
    tar -C "${WORKDIR}" -xzf "${tgz}"

    local extracted
    extracted="$(find "${WORKDIR}" -maxdepth 1 -type d -name "${repo_name}-*" | head -n 1)"
    [[ -n "${extracted}" && -d "${extracted}/tools/si" ]] || die "repo: failed to extract expected source tree from ${archive_url}"
    mv "${extracted}" "${out_dir}"
    return 0
  }

  if command -v git >/dev/null 2>&1; then
    if ! clone_repo "${REPO_URL}" "${REF}" "${local_dir}"; then
      log warn "repo: git fetch failed; trying GitHub tarball fallback"
      fetch_github_tarball "${REPO_URL}" "${REF}" "${local_dir}" || die "repo: failed to fetch repo (git failed, tarball fallback unavailable)"
    fi
  else
    log warn "repo: git not found; trying GitHub tarball fallback"
    fetch_github_tarball "${REPO_URL}" "${REF}" "${local_dir}" || die "git is required to fetch the repo (or use a https://github.com/<owner>/<repo>.git repo URL)"
  fi
  SOURCE_DIR="${local_dir}"
fi

required_go="${GO_VERSION}"
required_go="$(trim "${required_go}")"
if [[ -z "${required_go}" ]]; then
  required_go="$(parse_go_mod_required_version "${SOURCE_DIR}")"
fi

GO_BIN_KIND=""
go_bin="$(ensure_go "${required_go}" "${OS}" "${ARCH}")"

if [[ -z "${TMP_DIR}" ]]; then
  build_dir="$(mktemp_dir)"
else
  build_dir="${TMP_DIR%/}/si-build.$$"
  run mkdir -p "${build_dir}"
fi
if [[ "${DRY_RUN}" -eq 0 ]]; then
  # Ensure cleanup even if WORKDIR is empty (e.g. building from local checkout).
  trap 'rm -rf "${build_dir}" || true; cleanup' EXIT
fi
out_bin="${build_dir}/si"

build_si "${SOURCE_DIR}" "${go_bin}" "${out_bin}"
install_bin "${out_bin}" "${INSTALL_PATH}"

if [[ "${GO_BIN_KIND}" != "system" ]]; then
  if [[ "${LINK_GO_MODE}" == "always" ]] || [[ "${LINK_GO_MODE}" == "auto" ]]; then
    # If we had to download/cached Go, make it available next to si so:
    # - 'si build self' works in a fresh machine
    # - users can choose to add the install dir to PATH (we don't modify rc files)
    go_dir="$(dirname "${go_bin}")"
    install_symlink "${go_bin}" "$(dirname "${INSTALL_PATH}")/go"
    if [[ -x "${go_dir}/gofmt" ]]; then
      install_symlink "${go_dir}/gofmt" "$(dirname "${INSTALL_PATH}")/gofmt"
    fi
  fi
fi

ensure_docker_buildx "${OS}" "${ARCH}"
post_install "${INSTALL_PATH}"
configure_default_browser_setting
