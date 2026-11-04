$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$target = Join-Path $root 'third_party/lostcity'
$url = 'https://github.com/LostCityRS/Server.git'

if (Test-Path (Join-Path $target '.git')) {
    Write-Host "Lost City checkout already exists: $target"
    Write-Host "Review upstream changes before updating it manually."
    exit 0
}

if (Test-Path $target) {
    throw "Target exists but is not a git checkout: $target"
}

New-Item -ItemType Directory -Force -Path (Split-Path $target) | Out-Null
Write-Host "Cloning LostCityRS/Server into $target"
git clone $url $target
Write-Host "Bootstrap complete. Read third_party/lostcity/README.md and the upstream README before starting the server."
