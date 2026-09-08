param(
    [Parameter(Mandatory = $true)]
    [string] $EnvironmentPath,
    [switch] $AllowWrite
)

$ErrorActionPreference = 'Stop'
if (-not $AllowWrite) {
    throw 'Full integration tests write database rows and object-storage data. Re-run with -AllowWrite only for an isolated clone.'
}

$ProjectRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$ResolvedEnvironment = (Resolve-Path -LiteralPath $EnvironmentPath -ErrorAction Stop).Path

Push-Location $ProjectRoot
try {
    $env:XYMUSIC_INTEGRATION_ENV = $ResolvedEnvironment
    $env:XYMUSIC_ALLOW_WRITE_INTEGRATION = '1'
    # Keep disposable repeated E2E runs independent from persistent login throttles.
    # This only affects the isolated database selected by XYMUSIC_INTEGRATION_ENV.
    $env:XYMUSIC_RESET_TEST_RATE_LIMITS = '1'

	$GoScript = Join-Path $PSScriptRoot 'go.ps1'
	$Packages = & $GoScript list ./...
	if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
	& $GoScript test '-v' ./... -count=1
	if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
	& $GoScript vet ./...
    exit $LASTEXITCODE
}
finally {
    Pop-Location
}
