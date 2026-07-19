param(
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]] $GoArgs
)

$ErrorActionPreference = 'Stop'
$PortableGo = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..\RunTime\go\bin\go.exe'))
if (Test-Path -LiteralPath $PortableGo -PathType Leaf) {
    $Go = $PortableGo
}
else {
    $SystemGo = Get-Command go.exe -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($null -eq $SystemGo) {
        throw "Neither portable Go at $PortableGo nor go.exe on PATH was found"
    }
    $Go = $SystemGo.Source
}

& $Go @GoArgs
exit $LASTEXITCODE
