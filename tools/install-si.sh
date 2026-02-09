#!/usr/bin/env bash
set -euo pipefail

#
# si installer (macOS + Linux)
# - Builds from a local checkout when available, else clones and builds.
# - Ensures a compatible Go toolchain (downloads a user-local Go tarball if needed).
# - Installs to ~/.local/bin/si by default (or /usr/local/bin/si when run as root).
#

SCRIPT_NAME="install-si.sh"

usage() {
  cat <<'EOF'
Install the si CLI.

Usage:
  tools/install-si.sh [flags]

Flags:
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

  --os <linux|darwin>     Override OS detection (primarily for dry-run/testing)
  --arch <amd64|arm64>    Override arch detection (primarily for dry-run/testing)

  --tmp-dir <dir>         Temporary working directory (default: mktemp)
  --dry-run               Print actions without changing anything
  --quiet                 Reduce output
  --no-path-hint          Skip PATH guidance after install
  -h, --help              Show this help

Notes:
  - This script does not modify your shell rc files. If your install dir isn't on PATH,
    it prints the exact line to add for bash/zsh.
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
    warn) printf '%s\n' "warn: $*" >&2 ;;
    err)  printf '%s\n' "error: $*" >&2 ;;
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
  local hdr=()
  if [[ -n "${token}" ]]; then
    hdr=(-H "Authorization: Bearer ${token}")
  fi
  local json
  if ! json="$(curl -fsSL "${hdr[@]}" "${url}" 2>/dev/null)"; then
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
  local ver
  ver="$(grep -E '^go[[:space:]]+[0-9]+\.[0-9]+' "${gomod}" | head -n 1 | awk '{print $2}')"
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
      log info "go: using system go ${have} (${go_bin})"
      printf '%s' "${go_bin}"
      return 0
    fi
    log warn "go: system go is missing or too old (have ${have:-unknown}, need ${required})"
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
      log info "go: using cached go ${have} (${go_path})"
      printf '%s' "${go_path}"
      return 0
    fi
    log warn "go: cached go exists but version mismatch (have ${have:-unknown}, need ${required}); reinstalling"
  fi

  need_cmd curl
  need_cmd tar

  local tgz="go${required}.${os}-${arch}.tar.gz"
  local url="https://dl.google.com/go/${tgz}"
  local sha_url="${url}.sha256"

  log info "go: downloading ${url}"
  if [[ "${DRY_RUN}" -eq 1 ]]; then
    log info "go: would verify sha256 via ${sha_url}"
    log info "go: would install to ${dest}"
    printf '%s' "${go_path}"
    return 0
  fi

  local work
  work="$(mktemp_dir)"
  # Best-effort cleanup happens at the end of this function; on failure we may
  # leave the directory behind to preserve diagnostics.

  local tgz_path="${work}/${tgz}"
  curl -fsSL -o "${tgz_path}" "${url}"

  local expected
  expected="$(curl -fsSL "${sha_url}" | awk '{print $1}' | tr -d '\r\n')"
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

  log info "go: installed ${have} at ${go_path}"
  rm -rf "${work}" || true
  printf '%s' "${go_path}"
}

clone_repo() {
  local repo_url="$1"
  local ref="$2"
  local out_dir="$3"

  need_cmd git
  if [[ "${DRY_RUN}" -eq 1 ]]; then
    log info "git: would clone ${repo_url} to ${out_dir} (ref ${ref})"
    return 0
  fi

  # If ref looks like a commit SHA, do a normal clone then checkout.
  if [[ "${ref}" =~ ^[0-9a-fA-F]{7,40}$ ]]; then
    git clone "${repo_url}" "${out_dir}"
    git -C "${out_dir}" checkout "${ref}"
    return 0
  fi
  git clone --depth 1 --branch "${ref}" "${repo_url}" "${out_dir}"
}

build_si() {
  local root="$1"
  local go_bin="$2"
  local out_bin="$3"

  [[ -f "${root}/tools/si/go.mod" ]] || die "not a si repo root: ${root} (missing tools/si/go.mod)"
  if [[ "${DRY_RUN}" -eq 1 ]]; then
    log info "build: would run (${root}): ${go_bin} build -o ${out_bin} ./tools/si"
    return 0
  fi
  (cd "${root}" && "${go_bin}" build -o "${out_bin}" ./tools/si)
}

install_bin() {
  local src="$1"
  local dst="$2"
  local dst_dir
  dst_dir="$(dirname "${dst}")"

  if [[ "${DRY_RUN}" -eq 1 ]]; then
    log info "install: would install ${src} -> ${dst}"
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
  log info "installed: ${dst}"
  log info "verify:    ${dst} --help"
  log info ""

  if [[ "${NO_PATH_HINT}" -eq 0 ]]; then
    case ":${PATH}:" in
      *":${install_dir}:"*) ;;
      *)
        if [[ "${install_dir}" == "${HOME:-}/.local/bin" ]]; then
          log warn "~/.local/bin is not on PATH for this shell."
          log info "Add this to your shell rc (~/.bashrc or ~/.zshrc):"
          log info "  export PATH=\"${HOME}/.local/bin:\$PATH\""
          log info ""
        else
          log warn "install dir is not on PATH for this shell: ${install_dir}"
        fi
        ;;
    esac
  fi

  log info "First-time vault notes:"
  log info "  - Linux keyring support (recommended): sudo apt install -y libsecret-tools"
  log info "  - In a host repo: git submodule update --init --recursive"
  log info "  - Then: si vault status --env dev"
}

QUIET=0
DRY_RUN=0
FORCE=0
UNINSTALL=0
NO_PATH_HINT=0

REPO="Aureuma/si"
REPO_URL=""
REF="main"
VERSION=""
SOURCE_DIR=""
INSTALL_DIR=""
INSTALL_PATH=""
GO_MODE="auto"
GO_VERSION=""
OS_OVERRIDE=""
ARCH_OVERRIDE=""
TMP_DIR=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help) usage; exit 0 ;;
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

if [[ "${DRY_RUN}" -eq 0 ]]; then
  if [[ -n "${OS_OVERRIDE}" || -n "${ARCH_OVERRIDE}" ]]; then
    die "--os/--arch overrides are only supported with --dry-run (they do not make sense for a real install)"
  fi
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
      INSTALL_DIR="${HOME}/.local/bin"
    fi
  fi
  INSTALL_PATH="${INSTALL_DIR%/}/si"
fi

if [[ "${UNINSTALL}" -eq 1 ]]; then
  if [[ "${DRY_RUN}" -eq 1 ]]; then
    log info "uninstall: would remove ${INSTALL_PATH}"
    exit 0
  fi
  if [[ -e "${INSTALL_PATH}" ]]; then
    rm -f "${INSTALL_PATH}"
    log info "removed: ${INSTALL_PATH}"
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

WORKDIR=""
cleanup() {
  if [[ -n "${WORKDIR}" && "${DRY_RUN}" -eq 0 ]]; then
    rm -rf "${WORKDIR}" || true
  fi
}
trap cleanup EXIT

if [[ -z "${SOURCE_DIR}" ]]; then
  need_cmd git
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
  clone_repo "${REPO_URL}" "${REF}" "${local_dir}"
  SOURCE_DIR="${local_dir}"
fi

required_go="${GO_VERSION}"
required_go="$(trim "${required_go}")"
if [[ -z "${required_go}" ]]; then
  required_go="$(parse_go_mod_required_version "${SOURCE_DIR}")"
fi

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
post_install "${INSTALL_PATH}"
