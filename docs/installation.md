# Install and upgrade OC

[Documentation index](README.md) · [简体中文](zh_CN/installation.md)

Choose a release archive for a native executable, a [container](containers.md) for an isolated runtime, or a source build for development. Release archives do not require Go. OC does not currently publish installation packages through Homebrew, APT or RPM repositories; the local GoReleaser configuration is not a public package channel.

## Choose a release and platform

Use [GitHub Releases](https://github.com/soulteary/oc/releases). Tags follow `RELEASE.YYYY-MM-DDTHH-MM-SSZ` in UTC, rather than semantic versioning. The published `RELEASE.2026-10-07T14-10-00Z` tag is used below as a reproducible archive example; choose another published tag when upgrading.

Archive platform names are:

- Linux: `linux-amd64`, `linux-arm64`, `linux-arm` (ARMv7), `linux-386`, `linux-ppc64le`, `linux-s390x`.
- macOS: `darwin-amd64` for Intel, `darwin-arm64` for Apple Silicon.
- FreeBSD: `freebsd-amd64`.
- Windows: `windows-amd64`, `windows-arm64`.

`uname -m` reports `x86_64` for amd64 and commonly `aarch64` or `arm64` for arm64. Build targets and recorded runtime coverage are different; see [compatibility](compatibility.md).

Each version has eleven archives, `checksums.txt` and `release-manifest.json`. Windows archives use ZIP; other platforms use tar.gz. Archives contain the executable, licenses/notices, both project READMEs and the compatibility manifest. Checksums detect corruption and bind assets to the downloaded manifest; they are not detached signatures. Obtain both from the project's release page over HTTPS.

## Linux, macOS and FreeBSD

The following commands use Bash, `curl`, `awk` and `tar`. Set `PLATFORM` explicitly for your machine; do not leave it as `linux-amd64` on another architecture.

```bash
set -euo pipefail
TAG=RELEASE.2026-10-07T14-10-00Z
PLATFORM=linux-amd64
ARCHIVE="oc-$TAG-$PLATFORM.tar.gz"
WORK_DIR="$(mktemp -d)"
BASE_URL="https://github.com/soulteary/oc/releases/download/$TAG"
curl --fail --location "$BASE_URL/$ARCHIVE" --output "$WORK_DIR/$ARCHIVE"
curl --fail --location "$BASE_URL/checksums.txt" --output "$WORK_DIR/checksums.txt"
awk -v name="$ARCHIVE" '$2 == name { print }' "$WORK_DIR/checksums.txt" > "$WORK_DIR/selected-checksum.txt"
test "$(wc -l < "$WORK_DIR/selected-checksum.txt" | tr -d ' ')" = 1
```

Verify the selected archive before extracting it. The full checksum file also references the other platform archives, so verify only the selected line unless you downloaded every asset.

On Linux:

```sh
(cd "$WORK_DIR" && sha256sum -c selected-checksum.txt)
```

On macOS and FreeBSD with Perl's `shasum` installed:

```sh
(cd "$WORK_DIR" && shasum -a 256 -c selected-checksum.txt)
```

After a successful checksum:

```sh
tar -xzf "$WORK_DIR/$ARCHIVE" -C "$WORK_DIR"
mkdir -p "$HOME/.local/bin"
install -m 0755 "$WORK_DIR/oc-$TAG-$PLATFORM/oc" "$HOME/.local/bin/oc"
"$HOME/.local/bin/oc" --version
```

Add `$HOME/.local/bin` to your shell's `PATH` if necessary, then run `oc --help`. Keep the extracted licenses and notices when redistributing OC. The temporary download directory can be removed after verification and installation.

For macOS, set `PLATFORM=darwin-arm64` or `darwin-amd64` before downloading. If macOS blocks a downloaded executable, review the release and checksum, then use the OS's normal approval mechanism; do not disable system security globally. The project does not promise notarization or code signing.

If GitHub CLI is already available, `gh release download "$TAG" --repo soulteary/oc --pattern "$ARCHIVE" --pattern checksums.txt --dir "$WORK_DIR"` is an alternative download step. Check the archive in the same way.

## Windows PowerShell

Use PowerShell with `Invoke-WebRequest`, `Get-FileHash` and `Expand-Archive`. Set the platform to `windows-arm64` for an ARM64 machine.

```powershell
$ErrorActionPreference = 'Stop'
$Tag = 'RELEASE.2026-10-07T14-10-00Z'
$Platform = 'windows-amd64'
$Archive = "oc-$Tag-$Platform.zip"
$WorkDir = Join-Path ([IO.Path]::GetTempPath()) ([Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $WorkDir | Out-Null
$BaseUrl = "https://github.com/soulteary/oc/releases/download/$Tag"
Invoke-WebRequest "$BaseUrl/$Archive" -OutFile (Join-Path $WorkDir $Archive)
Invoke-WebRequest "$BaseUrl/checksums.txt" -OutFile (Join-Path $WorkDir 'checksums.txt')
$Lines = @(Get-Content (Join-Path $WorkDir 'checksums.txt') | Where-Object {
    $_ -match ('\s{2}' + [Regex]::Escape($Archive) + '$')
})
if ($Lines.Count -ne 1) { throw 'Expected exactly one archive checksum' }
$Expected = ($Lines[0] -split '\s+')[0]
$Actual = (Get-FileHash (Join-Path $WorkDir $Archive) -Algorithm SHA256).Hash.ToLowerInvariant()
if ($Actual -ne $Expected) { throw 'Archive checksum mismatch' }
Expand-Archive -LiteralPath (Join-Path $WorkDir $Archive) -DestinationPath $WorkDir
$InstallDir = Join-Path $HOME 'bin'
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
Copy-Item (Join-Path $WorkDir "oc-$Tag-$Platform/oc.exe") (Join-Path $InstallDir 'oc.exe')
& (Join-Path $InstallDir 'oc.exe') --version
```

Add `$InstallDir` to your user `PATH` to run `oc` from another directory. OC's default configuration directory is `oc` under the user profile, not `.mc`. [Configuration](configuration.md) explains overriding it.

## Build from source

Use Git and the Go toolchain declared in `go.mod` (`1.27.1` for this baseline). Make and CI set `GOTOOLCHAIN=local`; prepare the correct Go installation before building. Read [Go's installation instructions](https://go.dev/doc/install) when needed.

On Unix with Make:

```sh
git clone https://github.com/soulteary/oc.git
cd oc
make build
./oc --help
```

On Unix without Make, a direct developer build is available:

```sh
GOTOOLCHAIN=local go build -trimpath -o oc .
```

On Windows PowerShell:

```powershell
$env:GOTOOLCHAIN = 'local'
go build -trimpath -o oc.exe .
```

A plain `go build` produces development metadata; `make build` generates development metadata from the Git commit. Neither is a timestamp release build. Release stamping and clean-checkout packaging are described in [releasing OC](releasing.md).

The repository is `soulteary/oc`; its Go module remains `github.com/soulteary/mc`. Do not use the historical module name or MinIO download URLs as an OC installation source. [Development](development.md) covers checks and integration fixtures.

## Upgrade and rollback

`oc update` is disabled. Download and verify a chosen OC archive, preserve your existing configuration, and replace the executable manually. `oc --version` prints its release tag; `release-manifest.json` records the full source commit. An older binary may not understand newer configuration changes, so keep an appropriate configuration backup as well as the previous executable.

Validate aliases, TLS, copy/mirror behavior and retention operations in your deployment before rollout. For migration from mc, use [the migration guide](migration.md). For container upgrades, replace the pinned tag or digest and retain the configuration/data mounts.
