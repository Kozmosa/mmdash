#!/usr/bin/env bash
set -euo pipefail

repository_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
testenv_root="$repository_root/.testenv"
runtime_root="$testenv_root/runtime"

if ! command -v pixi >/dev/null 2>&1; then
  echo "Pixi is required. Install it from https://pixi.sh, then rerun this command." >&2
  exit 1
fi

mkdir -p "$runtime_root/tmp"

export APPDATA="$runtime_root/user/appdata"
export COREPACK_HOME="$testenv_root/cache/corepack"
export GOCACHE="$testenv_root/cache/go-build"
export GOMODCACHE="$testenv_root/cache/go-mod"
export GOPATH="$testenv_root/go"
export LOCALAPPDATA="$runtime_root/user/localappdata"
export NPM_CONFIG_CACHE="$testenv_root/cache/npm"
export NPM_CONFIG_USERCONFIG="$runtime_root/config/npmrc"
export PIXI_CACHE_DIR="$testenv_root/cache/pixi"
export PIXI_HOME="$testenv_root/pixi-home"
export PIXI_NO_CONFIG=1
export PNPM_HOME="$testenv_root/pnpm-home"
export RATTLER_CACHE_DIR="$testenv_root/cache/rattler"
export TEMP="$runtime_root/tmp"
export TMP="$runtime_root/tmp"
export TMPDIR="$runtime_root/tmp"
export UV_CACHE_DIR="$testenv_root/cache/uv"
export UV_PROJECT_ENVIRONMENT="$testenv_root/python"
export XDG_CACHE_HOME="$testenv_root/cache/xdg"
export XDG_CONFIG_HOME="$runtime_root/config/xdg"
export XDG_DATA_HOME="$runtime_root/user/xdg-data"
export XDG_STATE_HOME="$runtime_root/user/xdg-state"

if [[ $# -eq 0 ]]; then
  set -- doctor
fi

exec pixi run --manifest-path "$testenv_root/pixi.toml" "$@"
