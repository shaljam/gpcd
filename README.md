# gpcd - GoPro Cloud Downloader

A command-line tool for bulk downloads from GoPro Cloud. This fork of
[mvisonneau/gpcd](https://github.com/mvisonneau/gpcd) adds support for
TimeLapseVideo and Burst media, continues past media with no matching download
URL, and can skip completed downloads by comparing file sizes.

## Install

Install Go 1.19 or later, then install this fork:

```sh
go install github.com/shaljam/gpcd/cmd/gpcd@main
```

Use `@main` to get the changes in this fork. Inherited upstream release tags
pre-date these changes. The executable is installed in `GOBIN`, or
`$(go env GOPATH)/bin` when GOBIN is unset. Add that directory to PATH.
On Windows the default is `%USERPROFILE%\go\bin`.

To build and install a local checkout:

```sh
git clone https://github.com/shaljam/gpcd.git
cd gpcd
go install ./cmd/gpcd
```

This fork removes the dependency `replace` directive that prevented versioned
`go install` commands from working. Concurrent downloads, separate signed HEAD
URLs and resumable `.partN` files are handled inside this repository.

Upstream binaries, containers, Homebrew and Scoop packages do not include this
fork's changes.

## Download

Sign in to GoPro Cloud in your browser and obtain the token from the
`gp_access_token` cookie. In this setup, the token was available in that cookie,
rather than the request headers.

Open browser DevTools, go to **Application → Storage → Cookies** (Chromium) or
**Storage → Cookies** (Firefox), choose the GoPro site, and copy the **Value** of
`gp_access_token`. Use that value, without the cookie name or a `Bearer ` prefix,
as `GPCD_BEARER_TOKEN`. gpcd adds the Bearer prefix to its API requests.
If authentication stops working, sign in again and copy the refreshed cookie.

Keep the token private;
do not add it to source files or commits.

PowerShell:

```powershell
$env:GPCD_BEARER_TOKEN = "YOUR_TOKEN"
gpcd --local-path ./medias --from "2022-10-24T00:00:00Z" list
gpcd --skip-existing --local-path ./medias --from "2022-10-24T00:00:00Z" download
```

Bash:

```sh
export GPCD_BEARER_TOKEN="YOUR_TOKEN"
gpcd --skip-existing --local-path ./medias download
```

Global options go before the command. Run `gpcd --help` for the complete help.

| Option | Environment variable | Behavior |
| --- | --- | --- |
| `--skip-existing` | `GPCD_SKIP_EXISTING` | Skip final files whose size matches the remote size; default false |
| `--local-path` | `GPCD_LOCAL_PATH` | Output directory; default `./medias` |
| `--bearer-token` | `GPCD_BEARER_TOKEN` | GoPro Cloud authentication token |
| `--api-endpoint` | `GPCD_API_ENDPOINT` | API endpoint; default `https://api.gopro.com/media/` |
| `--user-agent` | `GPCD_USER_AGENT` | User-Agent used for GoPro API requests |
| `--from`, `--to` | — | Filter capture times using RFC3339 timestamps |
| `--max-concurrent-downloads` | — | Download workers per file; default CPU count |

## Skip existing files

`--skip-existing` compares the existing final file size with the remote
`Content-Length` from a HEAD request. Matching files are skipped without
downloading their contents. Missing files and files of different sizes are
downloaded. If a reliable remote size is unavailable, download proceeds normally.

This uses size equality only, with no checksum verification. Files with different
contents but equal sizes will be skipped. Use the same output directory when
rerunning downloads. Only final files count as completed; `.partN` files remain
resumable. Keep the worker count unchanged when resuming a partial download.

Enable the flag through the environment with `GPCD_SKIP_EXISTING=true`.

## Supported media and errors

- `Photo` and `Burst` use the photo download path.
- `Video` and `TimeLapseVideo` select a variation matching the media resolution.
- Media without a matching downloadable URL are logged with filename, ID, type
  and resolution, then skipped so the batch can continue.
- A batch with unavailable media reports the skipped count at the end and exits
  with a nonzero status. Files skipped because their sizes match are successful
  skips and do not cause a failure.
- HTTP failures include the status and media context. Other transfer errors stop
  the batch. Interrupted range downloads leave partial files for the next run.

See [download behavior and troubleshooting](docs/DOWNLOADS.md) and
[the change log](CHANGELOG.md) for details.

## Development

```sh
go test ./...
go vet ./...
go build ./cmd/gpcd
```

CI checks Go 1.19 and the stable Go toolchain on Linux and Windows, including
race tests on Linux. The inherited release workflow is restricted to the
upstream repository; configure release destinations and credentials before
enabling publishing for this fork.