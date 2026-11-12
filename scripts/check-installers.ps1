$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$version = (Get-Content -Raw (Join-Path $root 'mygo.json') | ConvertFrom-Json).version
$bundle = Join-Path $root 'build/native/windows-amd64'
$installer = Join-Path $bundle "framely-setup-$version-amd64.exe"
$id = 'io.screenpin.desktop'
$uninstall = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\$id"
$run = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
$startup = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run'
if (Test-Path $uninstall) { throw 'Installer checks require an isolated runner without Framely installed' }
$testRoot = Join-Path ([IO.Path]::GetTempPath()) ('Framely installer ' + [guid]::NewGuid())
New-Item -ItemType Directory -Path $testRoot | Out-Null
$shell = New-Object -ComObject WScript.Shell
function Assert($condition, $message) { if (!$condition) { throw $message } }
function Install([string]$destination = '') {
    $arguments = '/S'
    if ($destination) { $arguments += ' /D=' + $destination }
    $process = Start-Process -FilePath $installer -ArgumentList $arguments -Wait -PassThru
    Assert ($process.ExitCode -eq 0) "Installer exited $($process.ExitCode)"
}
function Uninstall([string]$directory) {
    $process = Start-Process -FilePath (Join-Path $directory 'Uninstall.exe') -ArgumentList ('/S _?=' + $directory) -Wait -PassThru
    Assert ($process.ExitCode -eq 0) "Uninstaller exited $($process.ExitCode)"
    Assert (!(Test-Path (Join-Path $directory 'Framely.exe'))) 'Application remains after uninstall'
    Assert (!(Test-Path $uninstall)) 'Uninstall registry remains'
}
function AssertAutostart([string]$executable, [bool]$configured) {
    if ($configured) {
        Assert ((Get-ItemPropertyValue $run $id) -eq ('"' + $executable + '" --hidden')) 'Autostart path not migrated'
        Assert ((Get-ItemPropertyValue $startup $id)[0] -eq 3) 'Disabled autostart was enabled'
    } else {
        foreach ($key in @($run, $startup)) {
            if (Test-Path -LiteralPath $key) {
                Assert ($null -eq (Get-Item -LiteralPath $key).GetValue($id, $null)) 'Installation created an autostart entry'
            }
        }
    }
}
try {
    foreach ($scenario in @('fresh', 'ScreenPin', 'ScreenPin without autostart', 'Framely', 'Framely without autostart', 'Custom App')) {
        $directoryName = $scenario -replace ' without autostart$', ''
        $hasAutostart = $scenario -ne 'fresh' -and !$scenario.EndsWith(' without autostart')
        $parent = Join-Path $testRoot $scenario
        $oldDir = Join-Path $parent $directoryName
        $newDir = if ($directoryName -eq 'ScreenPin' -or $scenario -eq 'fresh') { Join-Path $parent 'Framely' } else { $oldDir }
        if ($scenario -ne 'fresh') {
            New-Item -ItemType Directory -Path (Join-Path $oldDir 'workers/screenpin-ocr') -Force | Out-Null
            Copy-Item (Join-Path $bundle 'Framely.exe') (Join-Path $oldDir 'ScreenPin.exe')
            Set-Content (Join-Path $oldDir 'keep.txt') 'user data'
            Set-Content (Join-Path $oldDir 'workers/screenpin-capture.exe') 'old component'
            New-Item -Path $uninstall -Force | Out-Null
            New-ItemProperty -Path $uninstall -Name InstallLocation -Value $oldDir -PropertyType String -Force | Out-Null
            New-Item -Path $run -Force | Out-Null
            if ($hasAutostart) {
                New-ItemProperty -Path $run -Name $id -Value ('"' + (Join-Path $oldDir 'ScreenPin.exe') + '" --hidden') -PropertyType String -Force | Out-Null
                New-Item -Path $startup -Force | Out-Null
                New-ItemProperty -Path $startup -Name $id -Value ([byte[]](3,0,0,0,0,0,0,0,0,0,0,0)) -PropertyType Binary -Force | Out-Null
            }
            foreach ($folder in @([Environment]::GetFolderPath('DesktopDirectory'), [Environment]::GetFolderPath('Programs'))) {
                $link = $shell.CreateShortcut((Join-Path $folder 'ScreenPin.lnk'))
                $link.TargetPath = Join-Path $oldDir 'ScreenPin.exe'
                $link.Save()
            }
            Install
        } else { Install $newDir }
        Assert ((Get-ItemPropertyValue $uninstall InstallLocation) -ieq $newDir) 'Installation directory was not migrated correctly'
        $exe = Join-Path $newDir 'Framely.exe'
        # Windows PowerShell does not capture GUI-subsystem output reliably.
        # Use a pipe, just as the release packager does for this actual binary.
        & python -c "import subprocess,sys; got=subprocess.check_output([sys.argv[1], '--version'], text=True).strip(); expected='Framely '+sys.argv[2]+' (mygo)'; assert got == expected, repr(got)" $exe $version
        Assert ($LASTEXITCODE -eq 0) 'Installed command/version is incorrect'
        if ($scenario -ne 'fresh') {
            Assert (!(Test-Path (Join-Path $oldDir 'ScreenPin.exe'))) 'Old binary remains'
            Assert (!(Test-Path (Join-Path $oldDir 'workers/screenpin-ocr'))) 'Old OCR runtime remains'
            Assert (Test-Path (Join-Path $oldDir 'keep.txt')) 'User data was removed'
            foreach ($folder in @([Environment]::GetFolderPath('DesktopDirectory'), [Environment]::GetFolderPath('Programs'))) {
                Assert (!(Test-Path (Join-Path $folder 'ScreenPin.lnk'))) 'Old shortcut remains'
                Assert ($shell.CreateShortcut((Join-Path $folder 'Framely.lnk')).TargetPath -ieq $exe) 'Shortcut targets old path'
            }
        }
        AssertAutostart $exe $hasAutostart
        Install
        Assert (Test-Path $exe) 'Reinstall removed executable'
        AssertAutostart $exe $hasAutostart
        Uninstall $newDir
        Remove-ItemProperty -Path $run -Name $id -ErrorAction SilentlyContinue
        Remove-ItemProperty -Path $startup -Name $id -ErrorAction SilentlyContinue
        Write-Output "PASS Windows: $scenario, registry, command, shortcuts, autostart, reinstall, uninstall"
    }
} finally {
    # All files and registry entries were created by this isolated smoke test.
    Remove-Item $testRoot -Recurse -Force -ErrorAction SilentlyContinue
    Remove-Item $uninstall -Recurse -Force -ErrorAction SilentlyContinue
    Remove-ItemProperty -Path $run -Name $id -ErrorAction SilentlyContinue
    Remove-ItemProperty -Path $startup -Name $id -ErrorAction SilentlyContinue
    foreach ($folder in @([Environment]::GetFolderPath('DesktopDirectory'), [Environment]::GetFolderPath('Programs'))) {
        foreach ($name in @('ScreenPin.lnk', 'Framely.lnk')) {
            Remove-Item (Join-Path $folder $name) -Force -ErrorAction SilentlyContinue
        }
    }
}
