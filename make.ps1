#Requires -Version 5.1
[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [ValidateSet('run', 'installer', 'icon')]
    [string]$Task = 'run',

    [switch]$Rebuild
)

$ErrorActionPreference = 'Stop'
$root = $PSScriptRoot

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    $goBin = 'C:\Program Files\Go\bin'
    if (Test-Path $goBin) { $env:Path = "$goBin;$env:Path" }
    else { throw 'Go toolchain not found. Install it (winget install GoLang.Go) and retry.' }
}

$ldflags = '-s -w'

function Get-AppVersion {
    $nsi = Join-Path $root 'installer\installer.nsi'
    if (-not (Test-Path $nsi)) { return '0.0.0' }
    $m = [regex]::Match((Get-Content $nsi -Raw), '!define\s+APP_VERSION\s+"([^"]+)"')
    if ($m.Success) { return $m.Groups[1].Value }
    return '0.0.0'
}

function Invoke-GoBuild {
    param([string]$Output, [string]$Package, [string]$ExtraLd = '')
    Write-Host "  building $([System.IO.Path]::GetFileName($Output))" -ForegroundColor DarkGray
    $ld = ("$ldflags $ExtraLd").Trim()
    $previousCgoEnabled = [Environment]::GetEnvironmentVariable('CGO_ENABLED', 'Process')
    try {
        [Environment]::SetEnvironmentVariable('CGO_ENABLED', '0', 'Process')
        go build -trimpath "-ldflags=$ld" -o $Output $Package
        if ($LASTEXITCODE) { throw "build failed: $Package" }
    }
    finally {
        [Environment]::SetEnvironmentVariable('CGO_ENABLED', $previousCgoEnabled, 'Process')
    }
}

function Test-IconFile {
    param([string]$Path)
    if (-not (Test-Path $Path)) { return $false }
    try {
        $bytes = [System.IO.File]::ReadAllBytes($Path)
        if ($bytes.Length -lt 8) { return $false }
        return ($bytes[0] -eq 0 -and $bytes[1] -eq 0 -and $bytes[2] -eq 1 -and $bytes[3] -eq 0)
    }
    catch {
        return $false
    }
}

function Build-Icon {
    param([string]$OutFile = (Join-Path $root 'cmd\claude-tray\icon.ico'))

    Add-Type -AssemblyName System.Drawing
    $svg = Join-Path $root 'installer\logo.svg'
    if (-not (Test-Path $svg)) {
        throw "logo.svg is missing at $svg"
    }

    $browser = $null
    foreach ($c in @(
            "$env:ProgramFiles\Google\Chrome\Application\chrome.exe",
            "${env:ProgramFiles(x86)}\Google\Chrome\Application\chrome.exe",
            "$env:ProgramFiles\Microsoft\Edge\Application\msedge.exe",
            "${env:ProgramFiles(x86)}\Microsoft\Edge\Application\msedge.exe")) {
        if (Test-Path $c) { $browser = $c; break }
    }
    if (-not $browser) { throw 'Chrome or Edge is required to rasterize the SVG.' }

    $sizes = @(16, 20, 24, 32, 40, 48, 64, 128, 256)
    $work = Join-Path ([System.IO.Path]::GetTempPath()) ("claude-icon-" + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Force -Path $work | Out-Null
    try {
        $svgUri = ([uri]$svg).AbsoluteUri
        $pngs = @{}
        foreach ($s in $sizes) {
            $html = "<!doctype html><meta charset=utf-8><style>html,body{margin:0;background:transparent}img{width:${s}px;height:${s}px;display:block}</style><img src='$svgUri'>"
            $htmlPath = Join-Path $work "w$s.html"
            [System.IO.File]::WriteAllText($htmlPath, $html, (New-Object System.Text.UTF8Encoding($false)))
            $shot = Join-Path $work "i$s.png"
            $args = @('--headless=new', '--disable-gpu', '--hide-scrollbars',
                '--default-background-color=00000000', '--force-device-scale-factor=1',
                '--no-first-run', '--no-default-browser-check',
                "--user-data-dir=$work\p$s", "--screenshot=$shot", "--window-size=$s,$s",
                ([uri]$htmlPath).AbsoluteUri)
            $prev = $ErrorActionPreference; $ErrorActionPreference = 'Continue'
            try { & $browser @args 2>&1 | Out-Null } finally { $ErrorActionPreference = $prev }
            if (-not (Test-Path $shot)) { throw "Browser produced no screenshot at ${s}px" }
            $img = [System.Drawing.Image]::FromFile($shot)
            try {
                $bmp = New-Object System.Drawing.Bitmap($s, $s, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
                $g = [System.Drawing.Graphics]::FromImage($bmp)
                $g.InterpolationMode = 'HighQualityBicubic'; $g.Clear([System.Drawing.Color]::Transparent)
                $g.DrawImage($img, 0, 0, $s, $s); $g.Dispose()
                $ms = New-Object System.IO.MemoryStream
                $bmp.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png)
                $pngs[$s] = $ms.ToArray(); $ms.Dispose(); $bmp.Dispose()
            } finally { $img.Dispose() }
        }
        $out = New-Object System.IO.MemoryStream
        $bw = New-Object System.IO.BinaryWriter($out)
        $bw.Write([UInt16]0); $bw.Write([UInt16]1); $bw.Write([UInt16]$sizes.Count)
        $offset = 6 + (16 * $sizes.Count)
        foreach ($s in $sizes) {
            $d = $pngs[$s]; $dim = if ($s -ge 256) { 0 } else { $s }
            $bw.Write([Byte]$dim); $bw.Write([Byte]$dim); $bw.Write([Byte]0); $bw.Write([Byte]0)
            $bw.Write([UInt16]1); $bw.Write([UInt16]32); $bw.Write([UInt32]$d.Length); $bw.Write([UInt32]$offset)
            $offset += $d.Length
        }
        foreach ($s in $sizes) { $bw.Write($pngs[$s]) }
        $bw.Flush()
        New-Item -ItemType Directory -Force -Path (Split-Path $OutFile) | Out-Null
        [System.IO.File]::WriteAllBytes($OutFile, $out.ToArray())
        $bw.Dispose(); $out.Dispose()
        Write-Host "  wrote $OutFile" -ForegroundColor DarkGray
    } finally {
        Remove-Item -Recurse -Force $work -ErrorAction SilentlyContinue
    }
}

function Ensure-Icon {
    param([switch]$Force)
    $icon = Join-Path $root 'cmd\claude-tray\icon.ico'
    if ($Force -or -not (Test-IconFile $icon)) {
        Write-Host 'Generating icon...' -ForegroundColor Cyan
        Build-Icon -OutFile $icon
    }
}

function Invoke-Run {
    Push-Location $root
    try {
        Ensure-Icon
        $version = Get-AppVersion
        Write-Host 'Building...' -ForegroundColor Cyan
        Invoke-GoBuild -Output (Join-Path $root 'claude-proxy.exe') -Package '.' -ExtraLd "-X main.appVersion=$version"
        Invoke-GoBuild -Output (Join-Path $root 'claude-tray.exe') -Package './cmd/claude-tray' -ExtraLd "-H=windowsgui -X main.appVersion=$version"
        if (-not (Test-Path (Join-Path $root '.env'))) {
            Write-Host 'No .env found; copy .env.example to .env and set UPSTREAM_API_KEY.' -ForegroundColor Yellow
        }
        Write-Host 'Starting tray (Ctrl+C to stop)...' -ForegroundColor Green
        & (Join-Path $root 'claude-tray.exe')
    }
    finally { Pop-Location }
}

function Invoke-Installer {
    $makensis = $null
    foreach ($c in @("${env:ProgramFiles(x86)}\NSIS\makensis.exe", "$env:ProgramFiles\NSIS\makensis.exe")) {
        if (Test-Path $c) { $makensis = $c; break }
    }
    if (-not $makensis) { throw 'makensis.exe not found. Install NSIS from https://nsis.sourceforge.io.' }

    Ensure-Icon -Force:$Rebuild

    $staging = Join-Path $root 'installer\staging'
    $out = Join-Path $root 'installer\out'
    Remove-Item -Recurse -Force $staging -ErrorAction SilentlyContinue
    New-Item -ItemType Directory -Force -Path $staging, $out | Out-Null

    $version = Get-AppVersion
    Push-Location $root
    try {
        Write-Host "Building binaries (version $version)..." -ForegroundColor Cyan
        Invoke-GoBuild -Output (Join-Path $staging 'claude-proxy.exe') -Package '.' -ExtraLd "-X main.appVersion=$version"
        Invoke-GoBuild -Output (Join-Path $staging 'claude-tray.exe') -Package './cmd/claude-tray' -ExtraLd "-H=windowsgui -X main.appVersion=$version"
    }
    finally { Pop-Location }

    $setup = Join-Path $out 'Claude-Proxy-Setup.exe'
    $verify = Join-Path $root 'scripts\verify-installer.ps1'

    $built = $false
    for ($attempt = 1; $attempt -le 2; $attempt++) {
        Write-Host "Compiling installer with NSIS (attempt $attempt)..." -ForegroundColor Cyan
        Remove-Item $setup -ErrorAction SilentlyContinue
        & $makensis (Join-Path $root 'installer\installer.nsi')
        if ($LASTEXITCODE) { throw 'makensis failed' }
        if (-not (Test-Path $setup)) { throw "installer not produced at $setup" }

        & pwsh -NoProfile -File $verify -Path $setup
        if ($LASTEXITCODE -eq 0) { $built = $true; break }
        Write-Host 'Integrity check failed; rebuilding once...' -ForegroundColor Yellow
    }
    if (-not $built) {
        throw 'installer failed integrity verification twice.'
    }

    $mb = [math]::Round((Get-Item $setup).Length / 1MB, 1)
    Write-Host ""
    Write-Host "Done: $setup ($mb MB)" -ForegroundColor Green
    Write-Host 'Ship that one file.' -ForegroundColor Green
}

switch ($Task) {
    'run' { Invoke-Run }
    'installer' { Invoke-Installer }
    'icon' { Build-Icon; Write-Host 'Icon regenerated.' -ForegroundColor Green }
}

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    $goBin = 'C:\Program Files\Go\bin'
    if (Test-Path $goBin) {
        $env:Path = "$goBin;$env:Path"
    } else {
        throw 'Go toolchain not found. Install it and retry.'
    }
}

function Get-AppVersion {
    $nsi = Join-Path $root 'installer\installer.nsi'
    if (-not (Test-Path $nsi)) { return '0.0.0' }
    $m = [regex]::Match((Get-Content $nsi -Raw), '!define\s+APP_VERSION\s+"([^"]+)"')
    if ($m.Success) { return $m.Groups[1].Value }
    return '0.0.0'
}

function Invoke-GoBuild {
    param([string]$Output, [string]$Package = '.', [string]$ExtraLd = '')
    Write-Host "  building $([System.IO.Path]::GetFileName($Output))" -ForegroundColor DarkGray
    $ld = ("$ldflags $ExtraLd").Trim()
    $previousCgoEnabled = [Environment]::GetEnvironmentVariable('CGO_ENABLED', 'Process')
    try {
        [Environment]::SetEnvironmentVariable('CGO_ENABLED', '0', 'Process')
        go build -trimpath "-ldflags=$ld" -o $Output $Package
        if ($LASTEXITCODE) { throw "build failed: $Package" }
    }
    finally {
        [Environment]::SetEnvironmentVariable('CGO_ENABLED', $previousCgoEnabled, 'Process')
    }
}

switch ($Task) {
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
        $version = Get-AppVersion
        Write-Host '==> cross-compiling to dist/' -ForegroundColor Cyan
        $dist = Join-Path $root 'dist'
        New-Item -ItemType Directory -Force -Path $dist | Out-Null
        $targets = @(
            @{ OS = 'windows'; Arch = 'amd64'; Ext = '.exe' },
            @{ OS = 'linux'; Arch = 'amd64'; Ext = '' },
            @{ OS = 'darwin'; Arch = 'amd64'; Ext = '' },
            @{ OS = 'darwin'; Arch = 'arm64'; Ext = '' }
        )
        foreach ($t in $targets) {
            $out = Join-Path $dist ("claude-proxy-{0}-{1}{2}" -f $t.OS, $t.Arch, $t.Ext)
            Write-Host "    $out" -ForegroundColor DarkGray
            $env:GOOS = $t.OS
            $env:GOARCH = $t.Arch
            go build -trimpath "-ldflags=$ldflags -X main.appVersion=$version" -o $out .
            if ($LASTEXITCODE) { throw "build failed: $out" }
        }
        Remove-Item Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue
        Write-Host "release binaries in $dist" -ForegroundColor Green
    }
    'installer' { Invoke-Installer }
    'clean' {
        Remove-Item -Force -ErrorAction SilentlyContinue $binary
        Remove-Item -Recurse -Force -ErrorAction SilentlyContinue (Join-Path $root 'dist')
        Remove-Item -Recurse -Force -ErrorAction SilentlyContinue (Join-Path $root 'installer\staging')
        Write-Host 'cleaned' -ForegroundColor Green
    }
}
