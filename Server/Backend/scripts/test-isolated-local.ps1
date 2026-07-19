param(
    [Parameter(Mandatory = $true)]
    [string] $SourceEnvironmentPath,
    [string] $OutputDirectory = '',
    [int] $LegacyPort = 3101,
    [switch] $AllowWrite,
    [switch] $KeepEnvironment
)

$ErrorActionPreference = 'Stop'
if (-not $AllowWrite) {
    throw 'This command creates and writes an isolated PostgreSQL database and MinIO bucket. Re-run with -AllowWrite.'
}

$ProjectRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$ServerRoot = [System.IO.Path]::GetFullPath((Join-Path $ProjectRoot '..'))
$SourceEnvironment = (Resolve-Path -LiteralPath $SourceEnvironmentPath -ErrorAction Stop).Path
if ([string]::IsNullOrWhiteSpace($OutputDirectory)) {
    $OutputDirectory = Join-Path $ServerRoot ('xymusic-it-' + [DateTime]::UtcNow.ToString('yyyyMMdd-HHmmss'))
}
$OutputDirectory = [System.IO.Path]::GetFullPath($OutputDirectory)
$EnvironmentPath = Join-Path $OutputDirectory '.env'
$LegacyBaseUrl = 'http://127.0.0.1:' + $LegacyPort
$LegacyProcess = $null
$Created = $false
$ExitCode = 1

Push-Location $ProjectRoot
try {
    & (Join-Path $PSScriptRoot 'go.ps1') run ./cmd/xymusic-testenv create `
        -source $SourceEnvironment -output $OutputDirectory -port $LegacyPort
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    $Created = $true

    $LegacyExecutable = Join-Path $OutputDirectory 'xymusic.exe'
    if (-not (Test-Path -LiteralPath $LegacyExecutable -PathType Leaf)) {
        throw "Legacy executable was not copied to $LegacyExecutable"
    }
    $LegacyProcess = Start-Process -FilePath $LegacyExecutable `
        -ArgumentList 'serve' `
        -WorkingDirectory $OutputDirectory `
        -WindowStyle Hidden `
        -RedirectStandardOutput (Join-Path $OutputDirectory 'legacy.stdout.log') `
        -RedirectStandardError (Join-Path $OutputDirectory 'legacy.stderr.log') `
        -PassThru

    $Ready = $false
    for ($Attempt = 0; $Attempt -lt 60; $Attempt++) {
        if ($LegacyProcess.HasExited) {
            $LegacyProcess.WaitForExit()
            throw "Legacy comparison server exited with code $($LegacyProcess.ExitCode)"
        }
        try {
            $Response = Invoke-WebRequest -UseBasicParsing -Uri ($LegacyBaseUrl + '/health/live') -TimeoutSec 2
            if ($Response.StatusCode -eq 200) {
                $Ready = $true
                break
            }
        }
        catch {
            Start-Sleep -Milliseconds 500
        }
    }
    if (-not $Ready) {
        throw 'Legacy comparison server did not become ready'
    }

    & (Join-Path $PSScriptRoot 'test-isolated-full.ps1') `
        -EnvironmentPath $EnvironmentPath -LegacyBaseUrl $LegacyBaseUrl -AllowWrite
    $ExitCode = $LASTEXITCODE
}
finally {
    if ($null -ne $LegacyProcess -and -not $LegacyProcess.HasExited) {
        Stop-Process -Id $LegacyProcess.Id -Force -ErrorAction SilentlyContinue
        $LegacyProcess.WaitForExit()
    }
    if ($Created -and -not $KeepEnvironment) {
        & (Join-Path $PSScriptRoot 'go.ps1') run ./cmd/xymusic-testenv destroy -environment $EnvironmentPath
        if ($LASTEXITCODE -ne 0 -and $ExitCode -eq 0) {
            $ExitCode = $LASTEXITCODE
        }
    }
    Pop-Location
}
exit $ExitCode
