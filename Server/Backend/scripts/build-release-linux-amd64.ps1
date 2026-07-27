param(
    [string] $OutputDirectory = ''
)

$ErrorActionPreference = 'Stop'
$ProjectRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$ServerRoot = [System.IO.Path]::GetFullPath((Join-Path $ProjectRoot '..'))
if ([string]::IsNullOrWhiteSpace($OutputDirectory)) {
    $OutputDirectory = Join-Path $ProjectRoot 'release-linux-amd64'
}
$OutputDirectory = [System.IO.Path]::GetFullPath($OutputDirectory)
$AdminRoot = Join-Path $ServerRoot 'AdminWeb'

if (Test-Path -LiteralPath $OutputDirectory) {
    Remove-Item -LiteralPath $OutputDirectory -Recurse -Force
}
New-Item -ItemType Directory -Path $OutputDirectory -Force | Out-Null
try {
    $env:GOOS = 'linux'
    $env:GOARCH = 'amd64'
    $env:CGO_ENABLED = '0'
    Push-Location $ProjectRoot
    try {
        & (Join-Path $PSScriptRoot 'go.ps1') build '-trimpath' '-ldflags' '-s -w' '-o' (Join-Path $OutputDirectory 'xymusic') ./cmd/xymusic
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    }
    finally {
        Pop-Location
    }

    $AdminBuildScript = Join-Path $PSScriptRoot 'build-admin-web.ps1'
    & $AdminBuildScript -AdminRoot $AdminRoot
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

    $MigrationsOutput = Join-Path $OutputDirectory 'migrations'
    New-Item -ItemType Directory -Path $MigrationsOutput -Force | Out-Null
    Copy-Item -Path (Join-Path $ProjectRoot 'migrations\*') -Destination $MigrationsOutput -Recurse -Force
    $AdminDist = Join-Path $AdminRoot 'dist'
    $AdminOutput = Join-Path $OutputDirectory 'admin'
    New-Item -ItemType Directory -Path $AdminOutput -Force | Out-Null
    Copy-Item -Path (Join-Path $AdminDist '*') -Destination $AdminOutput -Recurse -Force

    Write-Host "Linux amd64 release directory created at $OutputDirectory"
}
finally {
    Remove-Item Env:GOOS -ErrorAction SilentlyContinue
    Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
    Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue
}
