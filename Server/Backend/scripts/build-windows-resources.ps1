param()

$ErrorActionPreference = 'Stop'
$ProjectRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$ResourceRoot = Join-Path $ProjectRoot 'cmd\xymusic'
$ResourceScript = Join-Path $ResourceRoot 'xymusic_windows.rc'
$ResourceObject = Join-Path $ResourceRoot 'xymusic_windows_amd64.syso'

$GoEnvironment = & (Join-Path $PSScriptRoot 'go.ps1') env GOOS GOARCH
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
$GoOS = [string] $GoEnvironment[0]
$GoArch = [string] $GoEnvironment[1]

if ($GoOS -ne 'windows') {
    throw "Windows resources can only be built for GOOS=windows; current GOOS is $GoOS"
}
if ($GoArch -ne 'amd64') {
    throw "The bundled XyMusic Windows icon resource supports GOARCH=amd64; current GOARCH is $GoArch"
}

$Windres = Get-Command windres.exe -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
if ($null -eq $Windres) {
    if (Test-Path -LiteralPath $ResourceObject -PathType Leaf) {
        Write-Host "windres.exe was not found; using committed $ResourceObject"
        return
    }
    throw 'windres.exe is required to generate the XyMusic Windows icon resource'
}

Push-Location $ResourceRoot
try {
    & $Windres.Source '--input-format=rc' '--output-format=coff' '--target=pe-x86-64' '-i' $ResourceScript '-o' $ResourceObject
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}
finally {
    Pop-Location
}

Write-Host "Windows icon resource created at $ResourceObject"
