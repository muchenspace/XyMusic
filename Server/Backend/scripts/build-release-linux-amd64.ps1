param(
    [string] $OutputDirectory = '',
    [Parameter(Mandatory = $true)]
    [string] $FFmpegArchive
)

$ErrorActionPreference = 'Stop'
$ProjectRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$ServerRoot = [System.IO.Path]::GetFullPath((Join-Path $ProjectRoot '..'))
if ([string]::IsNullOrWhiteSpace($OutputDirectory)) {
    $OutputDirectory = Join-Path $ProjectRoot 'release-linux-amd64'
}
$OutputDirectory = [System.IO.Path]::GetFullPath($OutputDirectory)
$projectPrefix = $ProjectRoot.TrimEnd([System.IO.Path]::DirectorySeparatorChar, [System.IO.Path]::AltDirectorySeparatorChar) + [System.IO.Path]::DirectorySeparatorChar
if (-not $OutputDirectory.StartsWith($projectPrefix, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw 'Linux release output directory must be inside BackendGo'
}
$FFmpegArchive = (Resolve-Path -LiteralPath $FFmpegArchive -ErrorAction Stop).Path
$AdminRoot = Join-Path $ServerRoot 'AdminWeb'
& (Join-Path $PSScriptRoot 'build-admin-web.ps1') -AdminRoot $AdminRoot
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

if (Test-Path -LiteralPath $OutputDirectory) {
    Remove-Item -LiteralPath $OutputDirectory -Recurse -Force
}
New-Item -ItemType Directory -Path $OutputDirectory -Force | Out-Null
$staging = Join-Path $ProjectRoot ('.xymusic-ffmpeg-staging-' + [Guid]::NewGuid().ToString('N'))

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

    Copy-Item -Path (Join-Path $ProjectRoot 'migrations') -Destination (Join-Path $OutputDirectory 'migrations') -Recurse -Force
    $AdminDist = Join-Path $AdminRoot 'dist'
    Copy-Item -Path $AdminDist -Destination (Join-Path $OutputDirectory 'admin') -Recurse -Force

    New-Item -ItemType Directory -Path $staging -Force | Out-Null
    tar -xJf $FFmpegArchive -C $staging
    $ffmpeg = Get-ChildItem -Path $staging -Recurse -File -Filter 'ffmpeg' | Select-Object -First 1
    $ffprobe = Get-ChildItem -Path $staging -Recurse -File -Filter 'ffprobe' | Select-Object -First 1
    if ($null -eq $ffmpeg -or $null -eq $ffprobe) {
        throw 'The supplied FFmpeg archive does not contain both ffmpeg and ffprobe'
    }
    $tools = Join-Path $OutputDirectory 'tools'
    New-Item -ItemType Directory -Path $tools -Force | Out-Null
    Copy-Item -LiteralPath $ffmpeg.FullName -Destination (Join-Path $tools 'ffmpeg') -Force
    Copy-Item -LiteralPath $ffprobe.FullName -Destination (Join-Path $tools 'ffprobe') -Force
    Copy-Item -LiteralPath (Join-Path $ffmpeg.DirectoryName 'GPLv3.txt') -Destination (Join-Path $OutputDirectory 'FFMPEG-GPLv3.txt') -Force -ErrorAction SilentlyContinue

    @'
# XyMusic reads only .env beside the xymusic executable.
# Do not place secrets in this example file. Start ./xymusic without .env to use the setup wizard.
# Relative paths are resolved from this directory.
# Bundled media tools: tools/ffmpeg and tools/ffprobe.
'@ | Set-Content -LiteralPath (Join-Path $OutputDirectory '.env.example') -Encoding ascii
    @'
#!/usr/bin/env sh
set -eu
chmod +x ./xymusic ./tools/ffmpeg ./tools/ffprobe
exec ./xymusic "$@"
'@ | Set-Content -LiteralPath (Join-Path $OutputDirectory 'run.sh') -Encoding ascii

    $archive = Join-Path $ProjectRoot 'xymusic-linux-amd64.tar.gz'
    if (Test-Path -LiteralPath $archive) { Remove-Item -LiteralPath $archive -Force }
    Push-Location (Split-Path -Parent $OutputDirectory)
    try {
        tar -czf $archive (Split-Path -Leaf $OutputDirectory)
    }
    finally {
        Pop-Location
    }
    Write-Host "Linux amd64 release created at $OutputDirectory"
    Write-Host "Archive created at $archive"
}
finally {
    Remove-Item -LiteralPath $staging -Recurse -Force -ErrorAction SilentlyContinue
    Remove-Item Env:GOOS -ErrorAction SilentlyContinue
    Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
    Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue
}
