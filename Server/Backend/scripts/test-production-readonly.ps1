param(
    [Parameter(Mandatory = $true)]
    [string] $EnvironmentPath
)

$ErrorActionPreference = 'Stop'
$ProjectRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$ResolvedEnvironment = (Resolve-Path -LiteralPath $EnvironmentPath -ErrorAction Stop).Path

Push-Location $ProjectRoot
try {
	$env:XYMUSIC_INTEGRATION_ENV = $ResolvedEnvironment
	Remove-Item Env:XYMUSIC_ALLOW_WRITE_INTEGRATION -ErrorAction SilentlyContinue
	$env:XYMUSIC_REQUIRE_FULL_API_PARITY = '1'

	& (Join-Path $PSScriptRoot 'go.ps1') test '-v' ./tests/integration `
		-run '^(TestProductionDependenciesAreCompatible|TestGinRoutesCoverEveryLegacyAPI)$' -count=1
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

    & (Join-Path $PSScriptRoot 'go.ps1') test '-v' `
        ./internal/modules/adminaudit `
        ./internal/modules/admincatalog `
        ./internal/modules/adminsettings `
        -count=1
    exit $LASTEXITCODE
}
finally {
    Pop-Location
}
