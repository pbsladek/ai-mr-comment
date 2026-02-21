param(
  [Parameter(ValueFromRemainingArguments = $true)]
  [string[]]$InstallArgs
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Assert-HttpsUrl {
  param([Parameter(Mandatory = $true)][string]$Url)
  $uri = [Uri]$Url
  if ($uri.Scheme -ne "https") {
    throw "Only https URLs are allowed: $Url"
  }
}

function Invoke-TlsRestMethod {
  param([Parameter(Mandatory = $true)][string]$Uri)
  Assert-HttpsUrl -Url $Uri
  return Invoke-RestMethod -Uri $Uri -SslProtocol Tls13
}

function Download-TlsFile {
  param(
    [Parameter(Mandatory = $true)][string]$Uri,
    [Parameter(Mandatory = $true)][string]$OutFile
  )
  Assert-HttpsUrl -Url $Uri
  Invoke-WebRequest -Uri $Uri -OutFile $OutFile -SslProtocol Tls13
}

$defaultRepo = "pbsladek/ai-mr-comment"
$repo = $defaultRepo
$version = "latest"
$forwardArgs = @()
$sawVersion = $false

for ($i = 0; $i -lt $InstallArgs.Count; $i++) {
  $arg = $InstallArgs[$i]
  switch -Regex ($arg) {
    '^(--repo|-repo)$' {
      if ($i + 1 -ge $InstallArgs.Count) {
        throw "missing value for --repo"
      }
      $i++
      $repo = $InstallArgs[$i]
      $forwardArgs += "-Repo"
      $forwardArgs += $repo
      continue
    }
    '^--repo=(.+)$' {
      $repo = $Matches[1]
      $forwardArgs += "-Repo"
      $forwardArgs += $repo
      continue
    }
    '^-repo:(.+)$' {
      $repo = $Matches[1]
      $forwardArgs += "-Repo"
      $forwardArgs += $repo
      continue
    }
    '^(--version|-version)$' {
      if ($i + 1 -ge $InstallArgs.Count) {
        throw "missing value for --version"
      }
      $i++
      $version = $InstallArgs[$i]
      $sawVersion = $true
      $forwardArgs += "-Version"
      $forwardArgs += "__RESOLVED_VERSION__"
      continue
    }
    '^--version=(.+)$' {
      $version = $Matches[1]
      $sawVersion = $true
      $forwardArgs += "-Version"
      $forwardArgs += "__RESOLVED_VERSION__"
      continue
    }
    '^-version:(.+)$' {
      $version = $Matches[1]
      $sawVersion = $true
      $forwardArgs += "-Version"
      $forwardArgs += "__RESOLVED_VERSION__"
      continue
    }
    default {
      $forwardArgs += $arg
    }
  }
}

if ($repo -notmatch '^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$') {
  throw "Invalid --repo value: $repo (expected owner/repo)"
}

$resolvedVersion = $version
if ($version -eq "latest") {
  $latest = Invoke-TlsRestMethod -Uri "https://api.github.com/repos/$repo/releases/latest"
  if (-not $latest.tag_name) {
    throw "Failed to resolve latest release tag for $repo"
  }
  $resolvedVersion = $latest.tag_name
}
if ($resolvedVersion -notmatch '^[A-Za-z0-9._-]+$') {
  throw "Invalid resolved version: $resolvedVersion"
}

$tagRef = Invoke-TlsRestMethod -Uri "https://api.github.com/repos/$repo/git/ref/tags/$resolvedVersion"
if (-not $tagRef.object -or -not $tagRef.object.sha) {
  throw "Failed to resolve tag ref for $repo $resolvedVersion"
}

if ($tagRef.object.type -eq "tag") {
  $tagObj = Invoke-TlsRestMethod -Uri "https://api.github.com/repos/$repo/git/tags/$($tagRef.object.sha)"
  if (-not $tagObj.object -or -not $tagObj.object.sha) {
    throw "Failed to resolve annotated tag target for $repo $resolvedVersion"
  }
  $installerSha = $tagObj.object.sha
} else {
  $installerSha = $tagRef.object.sha
}

$finalArgs = @()
foreach ($a in $forwardArgs) {
  $finalArgs += $a.Replace("__RESOLVED_VERSION__", $resolvedVersion)
}
if (-not $sawVersion) {
  $finalArgs += "-Version"
  $finalArgs += $resolvedVersion
}

$installerUrl = "https://raw.githubusercontent.com/$repo/$installerSha/scripts/install.ps1"
$tmp = New-TemporaryFile
try {
  Write-Host "Downloading pinned installer from $repo@$installerSha"
  Download-TlsFile -Uri $installerUrl -OutFile $tmp.FullName
  & $tmp.FullName @finalArgs
} finally {
  Remove-Item -Path $tmp.FullName -Force -ErrorAction SilentlyContinue
}
