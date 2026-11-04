$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$target = Join-Path $root 'third_party/lostcity'
if (-not (Test-Path (Join-Path $target '.git'))) { throw "Lost City checkout missing. Run tools/lostcity/bootstrap.ps1 first." }
Write-Host "Starting the pinned/local Lost City checkout."
Write-Host "Lab target tick profile: 200 ms (opt-in; only effective if supported by this upstream revision)."
Write-Host "The upstream launcher is authoritative; no source files are rewritten by this script."
Set-Location $target
node start.js
