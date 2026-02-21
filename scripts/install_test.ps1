Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$scriptPath = Join-Path $PSScriptRoot "install.ps1"
. $scriptPath

$script:LastCosignArgs = $null
function global:cosign {
  param([Parameter(ValueFromRemainingArguments = $true)]$Args)
  $script:LastCosignArgs = @($Args)
  return "ok"
}

$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("amc-install-test-" + [System.Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
  $artifact = Join-Path $tmp "artifact.zip"
  [System.IO.File]::WriteAllText($artifact, "hello")

  $actual = (Get-FileHash -Path $artifact -Algorithm SHA256).Hash.ToLowerInvariant()
  $checksums = Join-Path $tmp "checksums.txt"
  [System.IO.File]::WriteAllText($checksums, "$actual  artifact.zip`n")

  $resolved = Resolve-ExpectedChecksum -ChecksumsPath $checksums -Asset "artifact.zip"
  if ($resolved -ne $actual) {
    throw "Resolve-ExpectedChecksum returned unexpected value"
  }

  Verify-ArchiveChecksum -ChecksumsPath $checksums -ArchivePath $artifact -Asset "artifact.zip"

  $badChecksums = Join-Path $tmp "checksums-bad.txt"
  [System.IO.File]::WriteAllText($badChecksums, "deadbeef  artifact.zip`n")

  $failed = $false
  try {
    Verify-ArchiveChecksum -ChecksumsPath $badChecksums -ArchivePath $artifact -Asset "artifact.zip"
  } catch {
    $failed = $true
  }
  if (-not $failed) {
    throw "Expected Verify-ArchiveChecksum mismatch to fail"
  }

  # Verify-SignedChecksums uses exact tag identity for pinned versions.
  Verify-SignedChecksums -Repo "pbsladek/ai-mr-comment" -Version "v0.6.0" -ChecksumsPath $checksums -ChecksumsSigPath (Join-Path $tmp "checksums.txt.sig") -ChecksumsCertPath (Join-Path $tmp "checksums.txt.pem")
  if (-not ($script:LastCosignArgs -contains "--certificate-identity")) {
    throw "Expected --certificate-identity for pinned version"
  }
  if ($script:LastCosignArgs -contains "--certificate-identity-regexp") {
    throw "Did not expect --certificate-identity-regexp for pinned version"
  }
  if (-not ($script:LastCosignArgs -contains "https://github.com/pbsladek/ai-mr-comment/.github/workflows/release.yml@refs/tags/v0.6.0")) {
    throw "Expected exact pinned version identity"
  }

  # Verify-SignedChecksums uses regex identity for latest.
  Verify-SignedChecksums -Repo "pbsladek/ai-mr-comment" -Version "latest" -ChecksumsPath $checksums -ChecksumsSigPath (Join-Path $tmp "checksums.txt.sig") -ChecksumsCertPath (Join-Path $tmp "checksums.txt.pem")
  if (-not ($script:LastCosignArgs -contains "--certificate-identity-regexp")) {
    throw "Expected --certificate-identity-regexp for latest"
  }

  # Signature verification failure should fail-closed.
  function global:cosign {
    param([Parameter(ValueFromRemainingArguments = $true)]$Args)
    throw "cosign failed"
  }
  $failed = $false
  try {
    Verify-SignedChecksums -Repo "pbsladek/ai-mr-comment" -Version "v0.6.0" -ChecksumsPath $checksums -ChecksumsSigPath (Join-Path $tmp "checksums.txt.sig") -ChecksumsCertPath (Join-Path $tmp "checksums.txt.pem")
  } catch {
    $failed = $true
  }
  if (-not $failed) {
    throw "Expected Verify-SignedChecksums failure to fail-closed"
  }

  # URL scheme guard.
  Assert-HttpsUrl -Url "https://example.com/a"
  $failed = $false
  try {
    Assert-HttpsUrl -Url "http://example.com/a"
  } catch {
    $failed = $true
  }
  if (-not $failed) {
    throw "Expected Assert-HttpsUrl to reject http URL"
  }

  Write-Host "install.ps1 security tests passed"
} finally {
  Remove-Item -Path $tmp -Recurse -Force -ErrorAction SilentlyContinue
}
