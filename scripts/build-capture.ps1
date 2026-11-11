$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")
$destination = "resources/windows-amd64/workers"
New-Item -ItemType Directory -Force $destination | Out-Null
New-Item -ItemType Directory -Force "build" | Out-Null
$object = "/Fobuild/capture-windows.obj"
$executable = "/Fe$destination/framely-capture.exe"
& cl /nologo /std:c++20 /EHsc /O2 /MT /utf-8 $object $executable workers/capture/windows.cpp /link windowsapp.lib d3d11.lib dxgi.lib user32.lib shell32.lib
if ($LASTEXITCODE -ne 0) { throw "Windows capture component failed to build" }
