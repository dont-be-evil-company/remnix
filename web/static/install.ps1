#Requires -Version 5.1

<#
.SYNOPSIS
    remnix Installation Script for Windows

.DESCRIPTION
    Automatically downloads and installs the latest version of remnix from GitHub releases.

.PARAMETER SystemWide
    Install to system-wide location (requires Administrator privileges)

.EXAMPLE
    .\install.ps1

.EXAMPLE
    .\install.ps1 -SystemWide
#>

param(
    [switch]$SystemWide
)

$ErrorActionPreference = "Stop"

$Repo = "dont-be-evil-company/remnix"
$BinaryName = "remnix.exe"

function Write-Status {
    param([string]$Message)
    Write-Host "[INFO] $Message" -ForegroundColor Blue
}

function Write-Success {
    param([string]$Message)
    Write-Host "[SUCCESS] $Message" -ForegroundColor Green
}

function Write-WarningMsg {
    param([string]$Message)
    Write-Host "[WARNING] $Message" -ForegroundColor Yellow
}

function Write-ErrorMsg {
    param([string]$Message)
    Write-Host "[ERROR] $Message" -ForegroundColor Red
}

function Get-WindowsArchitecture {
    if ([Environment]::Is64BitOperatingSystem) {
        return "amd64"
    }
    return "NOT_SUPPORTED"
}

function Get-LatestVersion {
    try {
        $response = Invoke-WebRequest -Uri "https://github.com/$Repo/releases/latest" -MaximumRedirection 0 -ErrorAction SilentlyContinue
        if ($response.StatusCode -eq 302 -or $response.StatusCode -eq 301) {
            return ($response.Headers.Location -replace ".*/releases/tag/", "")
        }
        throw "Unexpected response from GitHub"
    } catch {
        Write-ErrorMsg "Failed to get latest version from GitHub: $($_.Exception.Message)"
        exit 1
    }
}

function Download-Binary {
    param(
        [string]$Version,
        [string]$Architecture
    )

    $downloadUrl = "https://github.com/$Repo/releases/download/$Version/remnix-windows-$Architecture.exe"
    Write-Status "Downloading $BinaryName $Version for windows-$Architecture..."

    $tempDir = New-TemporaryFile | ForEach-Object { Remove-Item $_; New-Item -ItemType Directory -Path $_ }
    $binaryPath = Join-Path $tempDir.FullName $BinaryName

    try {
        Invoke-WebRequest -Uri $downloadUrl -OutFile $binaryPath
        Write-Success "Download completed successfully"
        return $binaryPath
    } catch {
        Write-ErrorMsg "Failed to download binary from $downloadUrl"
        Remove-Item $tempDir.FullName -Recurse -Force -ErrorAction SilentlyContinue
        exit 1
    }
}

function Get-InstallLocation {
    if ($SystemWide) {
        if (-not ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole] "Administrator")) {
            Write-ErrorMsg "System-wide installation requires Administrator privileges."
            exit 1
        }
        return "C:\Program Files\remnix\$BinaryName"
    }

    $installDir = Join-Path $env:LOCALAPPDATA "remnix"
    if (-not (Test-Path $installDir)) {
        New-Item -ItemType Directory -Path $installDir -Force | Out-Null
    }
    return Join-Path $installDir $BinaryName
}

function Add-ToPath {
    param([string]$InstallPath)

    $scope = if ($SystemWide) { "Machine" } else { "User" }
    $installDir = Split-Path $InstallPath -Parent
    $currentPath = [Environment]::GetEnvironmentVariable("PATH", $scope)

    if ($currentPath -notlike "*$installDir*") {
        $newPath = if ($currentPath) { "$currentPath;$installDir" } else { $installDir }
        [Environment]::SetEnvironmentVariable("PATH", $newPath, $scope)
        Write-Status "Added $installDir to $scope PATH"
        Write-WarningMsg "You may need to restart your terminal for PATH changes to take effect"
    }
}

function Install-Binary {
    param(
        [string]$SourcePath,
        [string]$InstallPath
    )

    Write-Status "Installing $BinaryName to $InstallPath..."

    if (Test-Path $InstallPath) {
        $backupPath = "$InstallPath.backup.$(Get-Date -Format 'yyyyMMdd_HHmmss')"
        Write-WarningMsg "Backing up existing binary to $backupPath"
        Copy-Item $InstallPath $backupPath
    }

    $installDir = Split-Path $InstallPath -Parent
    if (-not (Test-Path $installDir)) {
        New-Item -ItemType Directory -Path $installDir -Force | Out-Null
    }

    Copy-Item $SourcePath $InstallPath
    Write-Success "$BinaryName installed successfully to $InstallPath"
    Add-ToPath -InstallPath $InstallPath
}

function Test-Installation {
    param([string]$InstallPath)

    if (Test-Path $InstallPath) {
        Write-Success "Installation verified successfully!"
        Write-Status "You can now run: remnix --version"
    } else {
        Write-ErrorMsg "Installation verification failed"
        exit 1
    }
}

function Main {
    Write-Status "Installing remnix..."

    $architecture = Get-WindowsArchitecture
    if ($architecture -eq "NOT_SUPPORTED") {
        Write-ErrorMsg "Unsupported architecture detected. Only 64-bit Windows is supported."
        exit 1
    }
    Write-Status "Detected architecture: $architecture"

    $version = Get-LatestVersion
    Write-Status "Latest version: $version"

    $tempBinary = Download-Binary -Version $version -Architecture $architecture
    $installPath = Get-InstallLocation
    Install-Binary -SourcePath $tempBinary -InstallPath $installPath

    Remove-Item $tempBinary -Force -ErrorAction SilentlyContinue
    Remove-Item (Split-Path $tempBinary -Parent) -Recurse -Force -ErrorAction SilentlyContinue

    Test-Installation -InstallPath $installPath
    Write-Success "remnix installation completed successfully!"
}

if ($env:OS -ne "Windows_NT") {
    Write-ErrorMsg "This script is designed for Windows systems only."
    Write-ErrorMsg "For Unix-like systems, use: curl -sSL /install.sh | sh"
    exit 1
}

Main
