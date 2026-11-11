$ErrorActionPreference = 'Stop'
$newDir = [IO.Path]::GetFullPath($env:FRAMELY_INSTALL_DIR)
$executable = Join-Path $newDir 'Framely.exe'
if (!(Test-Path -LiteralPath $executable -PathType Leaf)) { throw 'Missing installed Framely.exe' }
$oldDir = $env:FRAMELY_OLD_INSTALL_DIR
if (!$oldDir) { exit 0 }
$oldDir = [IO.Path]::GetFullPath($oldDir)
$sameDirectory = $oldDir -ieq $newDir
$oldExecutables = @((Join-Path $oldDir 'ScreenPin.exe'), (Join-Path $oldDir 'Framely.exe'))

# Keep the existing Run value and StartupApproved state; disabled autostart
# must not become enabled merely because the executable moved.
$runKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
$id = 'io.screenpin.desktop'
if (Test-Path $runKey) {
    $command = Get-ItemPropertyValue -LiteralPath $runKey -Name $id -ErrorAction SilentlyContinue
    foreach ($old in $oldExecutables) {
        if ($command -match ('^"?' + [regex]::Escape($old) + '"?(\s.*)?$')) {
            Set-ItemProperty -LiteralPath $runKey -Name $id -Value ('"' + $executable + '"' + $Matches[1])
            break
        }
    }
}

$shell = New-Object -ComObject WScript.Shell
foreach ($folder in @([Environment]::GetFolderPath('DesktopDirectory'), [Environment]::GetFolderPath('Programs'))) {
    foreach ($name in @('ScreenPin.lnk', 'Framely.lnk')) {
        $linkPath = Join-Path $folder $name
        if (!(Test-Path -LiteralPath $linkPath)) { continue }
        $link = $shell.CreateShortcut($linkPath)
        if ($oldExecutables -inotcontains $link.TargetPath) { continue }
        $target = Join-Path $folder 'Framely.lnk'
        $updated = $shell.CreateShortcut($target)
        $updated.TargetPath = $executable
        $updated.WorkingDirectory = $newDir
        $updated.Arguments = $link.Arguments
        $updated.IconLocation = $executable + ',0'
        $updated.Save()
        if ($linkPath -ine $target) { Remove-Item -LiteralPath $linkPath }
    }
}

# Remove only known application files. A user may have saved unrelated data
# in the old folder; never recursively delete that folder or its unknown files.
foreach ($name in @('ScreenPin.exe', 'Framely.exe', 'Uninstall.exe')) {
    if ($sameDirectory -and $name -ne 'ScreenPin.exe') { continue }
    $path = Join-Path $oldDir $name
    if (Test-Path -LiteralPath $path -PathType Leaf) { Remove-Item -LiteralPath $path }
}
$workers = Join-Path $oldDir 'workers'
foreach ($name in @('screenpin-capture.exe', 'screenpin-record.exe', 'screenpin-ffmpeg.exe', 'screenpin-ocr', 'media-manifest.json', 'media-licenses')) {
    if ($sameDirectory -and $name -in @('media-manifest.json', 'media-licenses')) { continue }
    $path = Join-Path $workers $name
    if (Test-Path -LiteralPath $path) { Remove-Item -LiteralPath $path -Recurse }
}
foreach ($folder in @($workers, $oldDir)) {
    if ((Test-Path -LiteralPath $folder -PathType Container) -and !(Get-ChildItem -LiteralPath $folder -Force | Select-Object -First 1)) {
        Remove-Item -LiteralPath $folder
    }
}
