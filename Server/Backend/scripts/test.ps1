$ErrorActionPreference = 'Stop'
$ProjectRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
Push-Location $ProjectRoot
try {
    & (Join-Path $PSScriptRoot 'go.ps1') test ./...
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    & (Join-Path $PSScriptRoot 'go.ps1') vet ./...
    exit $LASTEXITCODE
}
finally {
    Pop-Location
}
