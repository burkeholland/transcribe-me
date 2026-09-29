#Requires -Version 7.2
param()
. (Join-Path $PSScriptRoot 'common.ps1')
Add-Type -AssemblyName System.Drawing
$root = Split-Path $PSScriptRoot -Parent
New-Item -ItemType Directory -Force (Join-Path $root 'build') | Out-Null
$bitmap = [Drawing.Bitmap]::new(1024, 1024)
$graphics = [Drawing.Graphics]::FromImage($bitmap)
# The app icon is the in-app logo: logoIcon in frontend/src/main.ts, drawn in the light theme accent
# on a transparent background. Each bar is an SVG "M x y v length" path in its 32-unit viewBox.
$scale = 1024 / 32
$accent = [Drawing.Pen]::new([Drawing.Color]::FromArgb(255, 37, 99, 235), [single](2.6 * $scale))
try {
    $graphics.SmoothingMode = [Drawing.Drawing2D.SmoothingMode]::AntiAlias
    $graphics.Clear([Drawing.Color]::Transparent)
    $accent.StartCap = $accent.EndCap = [Drawing.Drawing2D.LineCap]::Round
    $bars = @(@(2.6, 13, 6), @(8.2, 9.5, 13), @(13.8, 4, 24), @(19.4, 8, 16), @(25, 5.5, 21), @(29.4, 13, 6))
    foreach ($bar in $bars) {
        $x = [single]($bar[0] * $scale)
        $graphics.DrawLine($accent, $x, [single]($bar[1] * $scale), $x, [single](($bar[1] + $bar[2]) * $scale))
    }
    $bitmap.Save((Join-Path $root 'build\appicon.png'), [Drawing.Imaging.ImageFormat]::Png)
    $sizes = @(16, 32, 48, 64, 128, 256)
    $images = [Collections.Generic.List[byte[]]]::new()
    foreach ($size in $sizes) {
        $small = [Drawing.Bitmap]::new($size, $size)
        $canvas = [Drawing.Graphics]::FromImage($small)
        $memory = [IO.MemoryStream]::new()
        try {
            $canvas.InterpolationMode = [Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
            $canvas.DrawImage($bitmap, 0, 0, $size, $size)
            $small.Save($memory, [Drawing.Imaging.ImageFormat]::Png)
            $images.Add($memory.ToArray())
        } finally { $memory.Dispose(); $canvas.Dispose(); $small.Dispose() }
    }
    New-Item -ItemType Directory -Force (Join-Path $root 'build\windows') | Out-Null
    $writer = [IO.BinaryWriter]::new([IO.File]::Create((Join-Path $root 'build\windows\icon.ico')))
    try {
        $writer.Write([uint16]0); $writer.Write([uint16]1); $writer.Write([uint16]$sizes.Count)
        $offset = 6 + 16 * $sizes.Count
        for ($i = 0; $i -lt $sizes.Count; $i++) {
            $dimension = if ($sizes[$i] -eq 256) { 0 } else { $sizes[$i] }
            $writer.Write([byte]$dimension); $writer.Write([byte]$dimension)
            $writer.Write([byte]0); $writer.Write([byte]0)
            $writer.Write([uint16]1); $writer.Write([uint16]32)
            $writer.Write([uint32]$images[$i].Length); $writer.Write([uint32]$offset)
            $offset += $images[$i].Length
        }
        foreach ($image in $images) { $writer.Write($image) }
    } finally { $writer.Dispose() }
} finally {
    $graphics.Dispose(); $bitmap.Dispose(); $accent.Dispose()
}
Write-Host 'Generated waveform icons: build\appicon.png and build\windows\icon.ico'
