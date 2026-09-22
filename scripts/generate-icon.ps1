#Requires -Version 7.2
param()
. (Join-Path $PSScriptRoot 'common.ps1')
Add-Type -AssemblyName System.Drawing
$root = Split-Path $PSScriptRoot -Parent
New-Item -ItemType Directory -Force (Join-Path $root 'build') | Out-Null
$bitmap = [Drawing.Bitmap]::new(1024, 1024)
$graphics = [Drawing.Graphics]::FromImage($bitmap)
$background = [Drawing.SolidBrush]::new([Drawing.Color]::FromArgb(255, 22, 26, 34))
$accent = [Drawing.Pen]::new([Drawing.Color]::FromArgb(255, 128, 232, 182), 52)
$text = [Drawing.Pen]::new([Drawing.Color]::FromArgb(255, 243, 243, 234), 45)
try {
    $graphics.SmoothingMode = [Drawing.Drawing2D.SmoothingMode]::AntiAlias
    $graphics.FillRectangle($background, 0, 0, 1024, 1024)
    $accent.StartCap = $accent.EndCap = [Drawing.Drawing2D.LineCap]::Round
    $text.StartCap = $text.EndCap = [Drawing.Drawing2D.LineCap]::Round
    $heights = @(110, 230, 370, 250, 135)
    for ($i = 0; $i -lt $heights.Count; $i++) {
        $x = 170 + $i * 78
        $graphics.DrawLine($accent, $x, [single](512 - $heights[$i] / 2), $x, [single](512 + $heights[$i] / 2))
    }
    $graphics.DrawLine($text, 630, 365, 850, 365)
    $graphics.DrawLine($text, 630, 510, 800, 510)
    $graphics.DrawLine($text, 630, 655, 755, 655)
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
    $graphics.Dispose(); $bitmap.Dispose(); $background.Dispose(); $accent.Dispose(); $text.Dispose()
}
Write-Host 'Generated original waveform-to-text icons: build\appicon.png and build\windows\icon.ico'
