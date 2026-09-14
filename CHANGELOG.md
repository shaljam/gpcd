# Change log

## Unreleased

- Add `--skip-existing` and `GPCD_SKIP_EXISTING` to skip final files whose
  local and remote sizes match. Enabled by default; use `--skip-existing=false` to disable.
- Support `TimeLapseVideo` using video resolution matching and `Burst` using
  the photo download path.
- Continue after media with no matching downloadable URL; log identifying
  details and report an incomplete batch at the end.
- Report HTTP errors when requesting download URLs and handle invalid requests.
- Fix versioned `go install` by removing the replaced downloader dependency,
  using the fork's module path, and handling transfers inside the repository.
- Preserve signed HEAD URLs, concurrent range downloads and partial-file resume.
- Validate ranges and transfer lengths; replace completed files through a
  temporary file so oversized previous files leave no trailing bytes.
- Cancel downloads on interruption and release interrupt-handler resources.
- Add regression tests and document installation, skipping and resume behavior.