param(
    [string]$Go = 'go',
    [string]$Output = '',
    [switch]$SkipTests
)

$ErrorActionPreference = 'Stop'
Push-Location (Split-Path $PSScriptRoot -Parent)

try {
    $arguments = @('run', './cmd/build')
    if ($Output) {
        $arguments += @('-output', $Output)
    }
    if ($SkipTests) {
        $arguments += '-skip-tests'
    }

    & $Go @arguments
    if ($LASTEXITCODE -ne 0) {
        throw 'Go build failed.'
    }
} finally {
    Pop-Location
}
