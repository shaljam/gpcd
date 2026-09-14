# Download behavior and troubleshooting

## URL selection

The search API can return media types beyond ordinary Photo and Video.
This fork recognizes Burst as a photo and TimeLapseVideo as a video.
Photos use the first download variation; videos require an exact quality match
with the media resolution. It does not substitute lower-quality video.

If no variation matches, the media is skipped with its filename, ID, type and
resolution. The remaining media are processed. At the end, a nonzero exit status
and skipped count indicate that the batch is incomplete.

A 401 or 403 when requesting download URLs is reported as an HTTP error, rather
than being mistaken for a missing variation. Refresh your bearer token if needed: sign in to GoPro Cloud, open the browser DevTools cookie storage, and copy the value of the `gp_access_token` cookie into `GPCD_BEARER_TOKEN`. In this setup the token came from this cookie, not from request headers. Do not include the cookie name or a Bearer prefix.

## Existing files

Run `gpcd --skip-existing --local-path ./medias download` to avoid transferring
files already present at the correct size. This still requests GoPro download
metadata and remote HEAD headers. It compares the local final file size with the
remote Content-Length, not hashes or timestamps.

If remote HEAD fails, is unsupported, omits Content-Length, or describes an
encoded representation, gpcd cannot confirm size equality and downloads normally.
A directory at the destination filename or a local file access error is reported.

Filename collisions are not distinguished by media ID: a same-name file with
the same size is skipped. Use separate output directories or date filters when
camera filenames have been reused.

## Transfers and resume

Downloads supporting byte ranges use concurrent workers and `.part1`, `.part2`,
etc. Existing partial bytes are reused. Keep `--max-concurrent-downloads`
unchanged across retries because the partial file ranges depend on that value.

Downloaded ranges are checked for the expected HTTP status, Content-Range and
byte count. An oversized partial file produces an error instead of being trusted.
Move incompatible partial files aside before restarting.

Completed ranges are merged into a temporary `.download` file and then moved to
the final filename. This replaces oversized old files without leaving trailing
bytes. Partial files are removed after successful completion.

Servers without range support use a full download to `.download`, then replace
the final file. These full transfers restart on retry. Ctrl+C cancels transfers;
range partials remain available for resume.

## Installation

The upstream go.mod used a replacement for a downloader fork. Go rejects
replace directives in modules installed with a version suffix. In addition, that
downloader fork declares the original project's module path, so directly
requiring the fork is insufficient.

This fork handles transfers internally and has no replace directive:
`go install github.com/shaljam/gpcd/cmd/gpcd@main`.

On Windows, stop an existing gpcd process before reinstalling; Windows can lock
the running executable. Ensure your Go bin directory is on PATH.