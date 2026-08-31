#requires -Version 5.1
[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [ValidateSet('build', 'run', 'check', 'clean', 'release')]
    [string]$Task = 'build'
)

$ErrorActionPreference = 'Stop'
$root = $PSScriptRoot
Set-Location $root

$binary = Join-Path $root 'claude-proxy.exe'

function Invoke-Build {
    Write-Host '==> building claude-proxy.exe' -ForegroundColor Cyan
    go build -trimpath -ldflags '-s -w' -o $binary .
    Write-Host "built $binary" -ForegroundColor Green
}

switch ($Task) {
    'build' { Invoke-Build }
    'run' {
        Invoke-Build
        Write-Host '==> running (Ctrl+C to stop)' -ForegroundColor Cyan
        & $binary
    }
    'check' {
        Write-Host '==> gofmt' -ForegroundColor Cyan
        $unformatted = gofmt -l .
        if ($unformatted) {
            Write-Host 'These files need gofmt:' -ForegroundColor Red
            $unformatted | ForEach-Object { Write-Host "    $_" -ForegroundColor Red }
            exit 1
        }
        Write-Host '==> go vet' -ForegroundColor Cyan
        go vet ./...
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
        Write-Host '==> go test' -ForegroundColor Cyan
        go test ./...
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
        Write-Host 'all checks passed' -ForegroundColor Green
    }
    'release' {
        Write-Host '==> cross-compiling to dist/' -ForegroundColor Cyan
        $dist = Join-Path $root 'dist'
        New-Item -ItemType Directory -Force -Path $dist | Out-Null
        $targets = @(
            @{ OS = 'windows'; Arch = 'amd64'; Ext = '.exe' },
            @{ OS = 'linux';   Arch = 'amd64'; Ext = '' },
            @{ OS = 'darwin';  Arch = 'amd64'; Ext = '' },
            @{ OS = 'darwin';  Arch = 'arm64'; Ext = '' }
        )
        foreach ($t in $targets) {
            $out = Join-Path $dist ("claude-proxy-{0}-{1}{2}" -f $t.OS, $t.Arch, $t.Ext)
            Write-Host "    $out" -ForegroundColor DarkGray
            $env:GOOS = $t.OS; $env:GOARCH = $t.Arch
            go build -trimpath -ldflags '-s -w' -o $out .
        }
        Remove-Item Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue
        Write-Host "release binaries in $dist" -ForegroundColor Green
    }
    'clean' {
        Remove-Item -Force -ErrorAction SilentlyContinue $binary
        Remove-Item -Recurse -Force -ErrorAction SilentlyContinue (Join-Path $root 'dist')
        Write-Host 'cleaned' -ForegroundColor Green
    }
}
