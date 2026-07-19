param(
    [Parameter(Mandatory = $true)]
    [string] $EnvironmentPath,
    [string] $LegacyBaseUrl = '',
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
    $env:XYMUSIC_REQUIRE_FULL_API_PARITY = '1'
    # Keep disposable repeated E2E runs independent from persistent login throttles.
    # This only affects the isolated database selected by XYMUSIC_INTEGRATION_ENV.
    $env:XYMUSIC_RESET_TEST_RATE_LIMITS = '1'
    if ([string]::IsNullOrWhiteSpace($LegacyBaseUrl)) {
        Remove-Item Env:XYMUSIC_LEGACY_BASE_URL -ErrorAction SilentlyContinue
    }
    else {
        $env:XYMUSIC_LEGACY_BASE_URL = $LegacyBaseUrl
    }

	$GoScript = Join-Path $PSScriptRoot 'go.ps1'
	$Packages = & $GoScript list ./...
	if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
	$SerialPackages = @(
		'xymusic/server/tests/integration',
		'xymusic/server/tests/parity',
		'xymusic/server/internal/workers/media'
	)
	$RegularPackages = @($Packages | Where-Object { $SerialPackages -notcontains $_ })
	& $GoScript test '-v' ./tests/integration -count=1
	if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
	# Repository integration tests share the isolated database. Serializing
	# packages prevents cleanup in one package from perturbing another package.
	& $GoScript test '-v' '-p=1' @RegularPackages -count=1
	if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
	& $GoScript test '-v' ./tests/parity -count=1
	if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
	& $GoScript test '-v' ./internal/workers/media -count=1
	if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
	& $GoScript vet ./...
    exit $LASTEXITCODE
}
finally {
    Pop-Location
}
