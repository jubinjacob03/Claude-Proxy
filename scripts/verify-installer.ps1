#Requires -Version 5.1
[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [string]$Path
)

$ErrorActionPreference = 'Stop'

if (-not (Test-Path -LiteralPath $Path)) {
    Write-Host "verify: file not found: $Path" -ForegroundColor Red
    exit 1
}

Add-Type -TypeDefinition @'
using System;
using System.IO;
public static class NsisCrc {
    static readonly uint[] Table = BuildTable();
    static uint[] BuildTable() {
        var t = new uint[256];
        for (uint i = 0; i < 256; i++) {
            uint c = i;
            for (int k = 0; k < 8; k++) c = ((c & 1) != 0) ? (0xEDB88320u ^ (c >> 1)) : (c >> 1);
            t[i] = c;
        }
        return t;
    }
    const int HeaderSkip = 512;
    public static bool Verify(string path, out long size, out uint computed, out uint stored) {
        byte[] b = File.ReadAllBytes(path);
        size = b.Length;
        computed = 0;
        stored = 0;
        if (b.Length < HeaderSkip + 4) return false;
        uint c = 0xFFFFFFFFu;
        for (int i = HeaderSkip; i < b.Length - 4; i++) c = Table[(c ^ b[i]) & 0xFF] ^ (c >> 8);
        computed = c ^ 0xFFFFFFFFu;
        stored = BitConverter.ToUInt32(b, b.Length - 4);
        return computed == stored;
    }
}
'@ -Language CSharp

$size = [long]0
$computed = [uint32]0
$stored = [uint32]0
$ok = [NsisCrc]::Verify($Path, [ref]$size, [ref]$computed, [ref]$stored)

$name = Split-Path $Path -Leaf
if ($ok) {
    Write-Host ("verify: {0} intact - CRC 0x{1:X8}, {2:N0} bytes" -f $name, $computed, $size) -ForegroundColor Green
    exit 0
}

Write-Host ("verify: {0} FAILED its integrity check" -f $name) -ForegroundColor Red
Write-Host ("  size on disk : {0:N0} bytes" -f $size)
Write-Host ("  CRC computed : 0x{0:X8}" -f $computed)
Write-Host ("  CRC embedded : 0x{0:X8}" -f $stored)
exit 1
