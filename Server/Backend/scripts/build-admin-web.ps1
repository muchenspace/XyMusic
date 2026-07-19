param(
    [string] $AdminRoot = ''
)

$ErrorActionPreference = 'Stop'
if ([string]::IsNullOrWhiteSpace($AdminRoot)) {
    $AdminRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..\AdminWeb'))
}
else {
    $AdminRoot = [System.IO.Path]::GetFullPath($AdminRoot)
}

$PackageFile = Join-Path $AdminRoot 'package.json'
if (-not (Test-Path -LiteralPath $PackageFile -PathType Leaf)) {
    throw "Admin web package was not found at $PackageFile"
}

$npm = Get-Command npm.cmd -ErrorAction SilentlyContinue
if ($null -eq $npm) {
    $npm = Get-Command npm -ErrorAction Stop
}

Push-Location $AdminRoot
try {
    & $npm.Source run build
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}
finally {
    Pop-Location
}

$AdminDist = Join-Path $AdminRoot 'dist'
if (-not (Test-Path -LiteralPath (Join-Path $AdminDist 'index.html') -PathType Leaf)) {
    throw "Admin web build did not produce $AdminDist\index.html"
}

Write-Host "Admin web distribution created at $AdminDist"
