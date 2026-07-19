param(
    [string] $OutputDirectory = ''
)

$ErrorActionPreference = 'Stop'
$ProjectRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$ServerRoot = [System.IO.Path]::GetFullPath((Join-Path $ProjectRoot '..'))
if ([string]::IsNullOrWhiteSpace($OutputDirectory)) {
    $OutputDirectory = Join-Path $ProjectRoot 'release'
}
$OutputDirectory = [System.IO.Path]::GetFullPath($OutputDirectory)
New-Item -ItemType Directory -Path $OutputDirectory -Force | Out-Null
$AdminRoot = Join-Path $ServerRoot 'AdminWeb'
& (Join-Path $PSScriptRoot 'build-admin-web.ps1') -AdminRoot $AdminRoot
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
$ToolsOutput = Join-Path $OutputDirectory 'tools'
if (Test-Path -LiteralPath $ToolsOutput) {
    Remove-Item -LiteralPath $ToolsOutput -Recurse -Force
}

Push-Location $ProjectRoot
try {
    & (Join-Path $PSScriptRoot 'build-windows-resources.ps1')
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    & (Join-Path $PSScriptRoot 'go.ps1') build '-trimpath' '-ldflags' '-s -w' '-o' (Join-Path $OutputDirectory 'xymusic.exe') ./cmd/xymusic
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}
finally {
    Pop-Location
}

$MigrationsOutput = Join-Path $OutputDirectory 'migrations'
New-Item -ItemType Directory -Path $MigrationsOutput -Force | Out-Null
Copy-Item -Path (Join-Path $ProjectRoot 'migrations\*') -Destination $MigrationsOutput -Recurse -Force
$AdminDist = Join-Path $AdminRoot 'dist'
$AdminOutput = Join-Path $OutputDirectory 'admin'
if (Test-Path -LiteralPath $AdminOutput) {
    Remove-Item -LiteralPath $AdminOutput -Recurse -Force
}
New-Item -ItemType Directory -Path $AdminOutput -Force | Out-Null
Copy-Item -Path (Join-Path $AdminDist '*') -Destination $AdminOutput -Recurse -Force
Write-Host "Go release created at $OutputDirectory"
