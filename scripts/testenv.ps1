param(
  [Parameter(ValueFromRemainingArguments = $true)]
  [string[]]$TaskArguments
)

$ErrorActionPreference = "Stop"
$repositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$testenvRoot = Join-Path $repositoryRoot ".testenv"
$runtimeRoot = Join-Path $testenvRoot "runtime"

if (-not $TaskArguments -or $TaskArguments.Count -eq 0) {
  $TaskArguments = @("doctor")
}

$pixi = Get-Command pixi -ErrorAction SilentlyContinue
if (-not $pixi) {
  Write-Error "Pixi is required. Install it from https://pixi.sh, then rerun this command."
}

$environment = @{
  APPDATA                    = Join-Path $runtimeRoot "user/appdata"
  COREPACK_HOME              = Join-Path $testenvRoot "cache/corepack"
  GOCACHE                    = Join-Path $testenvRoot "cache/go-build"
  GOMODCACHE                 = Join-Path $testenvRoot "cache/go-mod"
  GOPATH                     = Join-Path $testenvRoot "go"
  LOCALAPPDATA               = Join-Path $runtimeRoot "user/localappdata"
  NPM_CONFIG_CACHE           = Join-Path $testenvRoot "cache/npm"
  NPM_CONFIG_USERCONFIG      = Join-Path $runtimeRoot "config/npmrc"
  PIXI_CACHE_DIR             = Join-Path $testenvRoot "cache/pixi"
  PIXI_HOME                  = Join-Path $testenvRoot "pixi-home"
  PIXI_NO_CONFIG             = "1"
  PNPM_HOME                  = Join-Path $testenvRoot "pnpm-home"
  RATTLER_CACHE_DIR          = Join-Path $testenvRoot "cache/rattler"
  TEMP                       = Join-Path $runtimeRoot "tmp"
  TMP                        = Join-Path $runtimeRoot "tmp"
  TMPDIR                     = Join-Path $runtimeRoot "tmp"
  UV_CACHE_DIR               = Join-Path $testenvRoot "cache/uv"
  UV_PROJECT_ENVIRONMENT     = Join-Path $testenvRoot "python"
  XDG_CACHE_HOME             = Join-Path $testenvRoot "cache/xdg"
  XDG_CONFIG_HOME            = Join-Path $runtimeRoot "config/xdg"
  XDG_DATA_HOME              = Join-Path $runtimeRoot "user/xdg-data"
  XDG_STATE_HOME             = Join-Path $runtimeRoot "user/xdg-state"
}

$previousEnvironment = @{}
foreach ($entry in $environment.GetEnumerator()) {
  $previousEnvironment[$entry.Key] = [Environment]::GetEnvironmentVariable(
    $entry.Key,
    [EnvironmentVariableTarget]::Process
  )
  [Environment]::SetEnvironmentVariable(
    $entry.Key,
    $entry.Value,
    [EnvironmentVariableTarget]::Process
  )
}

New-Item -ItemType Directory -Force $environment.TEMP | Out-Null

$exitCode = 1
try {
  & $pixi.Source run --manifest-path (Join-Path $testenvRoot "pixi.toml") @TaskArguments
  $exitCode = $LASTEXITCODE
} finally {
  foreach ($entry in $previousEnvironment.GetEnumerator()) {
    [Environment]::SetEnvironmentVariable(
      $entry.Key,
      $entry.Value,
      [EnvironmentVariableTarget]::Process
    )
  }
}

exit $exitCode
