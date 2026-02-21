param(
  [string]$Repo = "pbsladek/ai-mr-comment",
  [string]$Version = "latest",
  [string]$InstallDir = "$env:LOCALAPPDATA\Programs\ai-mr-comment",
  [switch]$AddToPath,
  [switch]$RunVersionCheck,
  [bool]$VerifySignature = $true
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$appName = "ai-mr-comment"

function Resolve-ExpectedChecksum {
  param(
    [Parameter(Mandatory = $true)][string]$ChecksumsPath,
    [Parameter(Mandatory = $true)][string]$Asset
  )

  foreach ($line in Get-Content -Path $ChecksumsPath) {
    if ($line -match '^\s*([a-fA-F0-9]{64})\s+(.+)\s*$') {
      if ($Matches[2] -eq $Asset) {
        return $Matches[1].ToLowerInvariant()
      }
    }
  }
  throw "Could not find checksum entry for $Asset in checksums.txt"
}

function Verify-ArchiveChecksum {
  param(
    [Parameter(Mandatory = $true)][string]$ChecksumsPath,
    [Parameter(Mandatory = $true)][string]$ArchivePath,
    [Parameter(Mandatory = $true)][string]$Asset
  )

  $expected = Resolve-ExpectedChecksum -ChecksumsPath $ChecksumsPath -Asset $Asset
  $actual = (Get-FileHash -Path $ArchivePath -Algorithm SHA256).Hash.ToLowerInvariant()
  if ($actual -ne $expected) {
    throw "Checksum verification failed for $Asset. Expected: $expected Actual: $actual"
  }
}

function Verify-SignedChecksums {
  param(
    [Parameter(Mandatory = $true)][string]$Repo,
    [Parameter(Mandatory = $true)][string]$Version,
    [Parameter(Mandatory = $true)][string]$ChecksumsPath,
    [Parameter(Mandatory = $true)][string]$ChecksumsSigPath,
    [Parameter(Mandatory = $true)][string]$ChecksumsCertPath
  )

  if ($Version -eq "latest") {
    $identityRegex = "https://github.com/$Repo/.github/workflows/release.yml@refs/tags/.*"
    & cosign verify-blob $ChecksumsPath `
      --certificate $ChecksumsCertPath `
      --signature $ChecksumsSigPath `
      --certificate-identity-regexp $identityRegex `
      --certificate-oidc-issuer "https://token.actions.githubusercontent.com" | Out-Null
  } else {
    $identity = "https://github.com/$Repo/.github/workflows/release.yml@refs/tags/$Version"
    & cosign verify-blob $ChecksumsPath `
      --certificate $ChecksumsCertPath `
      --signature $ChecksumsSigPath `
      --certificate-identity $identity `
      --certificate-oidc-issuer "https://token.actions.githubusercontent.com" | Out-Null
  }
}

function Assert-HttpsUrl {
  param([Parameter(Mandatory = $true)][string]$Url)
  $uri = [Uri]$Url
  if ($uri.Scheme -ne "https") {
    throw "Only https URLs are allowed: $Url"
  }
}

function Download-File {
  param(
    [Parameter(Mandatory = $true)][string]$Url,
    [Parameter(Mandatory = $true)][string]$OutFile
  )
  Assert-HttpsUrl -Url $Url
  Invoke-WebRequest -Uri $Url -OutFile $OutFile -SslProtocol Tls13
}

function Get-Arch {
  $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
  switch ($arch) {
    "X64" { return "x86_64" }
    "Arm64" { return "arm64" }
    default { throw "Unsupported architecture: $arch" }
  }
}

function Add-UserPathEntry {
  param([Parameter(Mandatory = $true)][string]$PathEntry)

  $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
  $paths = @()
  if ($userPath) {
    $paths = $userPath -split ';' | Where-Object { $_ -and $_.Trim() -ne "" }
  }
  if ($paths -contains $PathEntry) {
    return
  }
  $newPath = ($paths + $PathEntry) -join ';'
  [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
  Write-Host "Added to user PATH: $PathEntry"
  Write-Host "Open a new terminal session to pick up PATH changes."
}

function Main {
  $arch = Get-Arch
  $asset = "${appName}_Windows_${arch}.zip"
  $checksumsAsset = "checksums.txt"
  $checksumsSigAsset = "checksums.txt.sig"
  $checksumsCertAsset = "checksums.txt.pem"

  if ($Version -eq "latest") {
    $url = "https://github.com/$Repo/releases/latest/download/$asset"
    $checksumsUrl = "https://github.com/$Repo/releases/latest/download/$checksumsAsset"
    $checksumsSigUrl = "https://github.com/$Repo/releases/latest/download/$checksumsSigAsset"
    $checksumsCertUrl = "https://github.com/$Repo/releases/latest/download/$checksumsCertAsset"
  } else {
    $url = "https://github.com/$Repo/releases/download/$Version/$asset"
    $checksumsUrl = "https://github.com/$Repo/releases/download/$Version/$checksumsAsset"
    $checksumsSigUrl = "https://github.com/$Repo/releases/download/$Version/$checksumsSigAsset"
    $checksumsCertUrl = "https://github.com/$Repo/releases/download/$Version/$checksumsCertAsset"
  }

  Assert-HttpsUrl -Url $url
  Assert-HttpsUrl -Url $checksumsUrl
  Assert-HttpsUrl -Url $checksumsSigUrl
  Assert-HttpsUrl -Url $checksumsCertUrl

  $tmpRoot = New-Item -ItemType Directory -Path ([System.IO.Path]::GetTempPath()) -Name ("$appName-install-" + [System.Guid]::NewGuid().ToString())
  try {
    $archivePath = Join-Path $tmpRoot.FullName $asset
    $checksumsPath = Join-Path $tmpRoot.FullName $checksumsAsset
    $checksumsSigPath = Join-Path $tmpRoot.FullName $checksumsSigAsset
    $checksumsCertPath = Join-Path $tmpRoot.FullName $checksumsCertAsset
    $extractDir = Join-Path $tmpRoot.FullName "extract"
    New-Item -ItemType Directory -Path $extractDir | Out-Null

    if ($VerifySignature) {
      if (-not (Get-Command cosign -ErrorAction SilentlyContinue)) {
        throw "cosign is required for signed checksum verification. Install cosign or run with -VerifySignature:`$false (unsafe; trusted offline/internal mirrors only)"
      }
      Write-Host "Downloading $checksumsSigUrl"
      Download-File -Url $checksumsSigUrl -OutFile $checksumsSigPath
      Write-Host "Downloading $checksumsCertUrl"
      Download-File -Url $checksumsCertUrl -OutFile $checksumsCertPath
    }

    Write-Host "Downloading $checksumsUrl"
    Download-File -Url $checksumsUrl -OutFile $checksumsPath

    if ($VerifySignature) {
      Verify-SignedChecksums -Repo $Repo -Version $Version -ChecksumsPath $checksumsPath -ChecksumsSigPath $checksumsSigPath -ChecksumsCertPath $checksumsCertPath
    }

    Write-Host "Downloading $url"
    Download-File -Url $url -OutFile $archivePath

    Verify-ArchiveChecksum -ChecksumsPath $checksumsPath -ArchivePath $archivePath -Asset $asset

    Write-Host "Extracting $asset"
    Expand-Archive -Path $archivePath -DestinationPath $extractDir -Force

    $binary = Get-ChildItem -Path $extractDir -Recurse -Filter "$appName.exe" | Select-Object -First 1
    if (-not $binary) {
      throw "Failed to find $appName.exe in archive $asset"
    }

    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    $targetPath = Join-Path $InstallDir "$appName.exe"
    Copy-Item -Path $binary.FullName -Destination $targetPath -Force

    Write-Host "Installed $appName to $targetPath"

    if ($AddToPath) {
      Add-UserPathEntry -PathEntry $InstallDir
    }

    if ($RunVersionCheck) {
      & $targetPath --version
    }
  } finally {
    Remove-Item -Path $tmpRoot.FullName -Recurse -Force -ErrorAction SilentlyContinue
  }
}

if ($MyInvocation.InvocationName -ne '.') {
  Main
}
