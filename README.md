# darwin-photos

[![GitHub stars](https://img.shields.io/github/stars/cleanexit0/darwin-photos)](https://github.com/cleanexit0/darwin-photos/stargazers)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)
[![Homebrew](https://img.shields.io/badge/homebrew-available-orange)](https://github.com/cleanexit0/homebrew-tap)

**Back up iCloud Photos to external storage — without downloading everything to your Mac first.**

```
$ darwin-photos backup /Volumes/External/photos-backup
Scanning library... 37,314 photos (326.8 GB)
Scanning output directory... 31,000 already backed up
Local copies: 1,203 | Cloud downloads: 5,111

[████████████████░░░░] 67% | 218.9GB/326.8GB | 156 MB/s | ETA 2m
```

## Why this exists

You have 500GB of photos in iCloud. Your Mac has 512GB total. You use "Optimize Mac Storage" because you have to.

Now you want a backup on an external drive.

**Your options today:**

| Method | Pain level |
|--------|-----------|
| Turn off "Optimize Storage", wait days for download, copy to external, turn it back on, wait days for re-upload | Brutal |
| Download from icloud.com manually | Flaky, slow, 1000-photo limit |
| Use `icloudpd` | Downloads everything from iCloud API (can't use local cache), [~5 MB/s even on gigabit](https://github.com/icloud-photos-downloader/icloud_photos_downloader/issues/1215) |
| Use `osxphotos --download-missing` | [AppleScript is inherently slow](https://github.com/RhetTbull/osxphotos/discussions/1388), can crash Photos.app |

**darwin-photos approach:**

1. Reads your local Photos database directly (instant)
2. Local files → copies directly to external (fast, ~150MB/s)
3. Cloud-only files → downloads via iCloud API (parallel, with progress)
4. "Optimize Storage" stays ON the whole time

## Installation

```bash
brew install cleanexit0/tap/darwin-photos
```

Or download from [releases](https://github.com/cleanexit0/darwin-photos/releases).

## Quick start

```bash
# See what's in your library
darwin-photos stats

# List recent photos
darwin-photos ls

# Backup everything to external drive
darwin-photos backup /Volumes/External/photos-backup
```

For cloud-only photos, you'll need to import cookies once:
```bash
# Export cookies from icloud.com using a browser extension, then:
darwin-photos export import-cookies cookies.txt
```

## Commands

| Command | Description |
|---------|-------------|
| `stats` | Library overview: counts, sizes, local vs cloud |
| `ls` | List photos with filters (date, album, type, cloud/local) |
| `cat` | Show detailed metadata for a specific photo |
| `albums` | List all albums |
| `backup` | Incremental backup to a directory (local + cloud) |
| `export` | Export specific photos directly from iCloud |
| `sync` | Download cloud photos into your Photos library |

## Example output

```
$ darwin-photos stats
iCloud Storage (counts toward quota):            219.4GB
  Originals:                                     130.8GB
  Cloud derivatives:                              88.6GB

Local Cache Only (not in iCloud):                107.4GB

By type:
  Photos:  37182 items     320.2GB
  Videos:    132 items       6.6GB

By availability:
  Downloaded:    2267 items      18.5GB
  Cloud-only:   35047 items     308.3GB
```

```
$ darwin-photos ls --limit 5
UUID                                  Filename              Type   Size       Date                 Status
F8A3B2C1-...                          IMG_4521.HEIC         photo  4.2MB      2024-12-15 14:23     Local
A1B2C3D4-...                          IMG_4520.HEIC         photo  3.8MB      2024-12-15 14:22     Cloud
...
```

## How it works

darwin-photos reads `Photos.sqlite` directly — the same database Apple Photos uses. This means:

- **No re-authentication** with iCloud (your Mac already has access)
- **Instant library scanning** (it's just a SQLite query)
- **Accurate cloud/local status** (reads the actual flags Apple uses)

For cloud downloads, it uses the same iCloud Photos API that the web interface uses, but with parallel workers and resume capability.

## Comparison with alternatives

| Feature | darwin-photos | [icloudpd](https://github.com/icloud-photos-downloader/icloud_photos_downloader) | [osxphotos](https://github.com/RhetTbull/osxphotos) |
|---------|--------------|-----------|-----------|
| Language | Go | Python | Python |
| Download cloud-only photos | Yes | Yes | Yes ([slow AppleScript](https://github.com/RhetTbull/osxphotos/discussions/1388), can crash Photos) |
| Use local cache (skip download) | Yes | No (always downloads from iCloud) | Yes |
| Parallel downloads | Yes (goroutines, configurable workers) | [Single-threaded by default](https://github.com/icloud-photos-downloader/icloud_photos_downloader/issues/1215), optional threading is [buggy](https://github.com/boredazfcuk/docker-icloudpd/blob/master/CONFIGURATION.md) | No |
| Keeps "Optimize Storage" on | Yes | N/A (no local library) | Yes |
| Cross-platform | No (macOS only) | Yes (Linux, Windows, macOS) | No (macOS only) |
| Auth method | Browser cookies | iCloud login + 2FA | Local library access |

## Requirements

- macOS (reads Apple's Photos database format)
- Full Disk Access permission (System Settings → Privacy → Full Disk Access)
- For cloud photos: cookies from icloud.com

## Disclaimer

This is an unofficial tool. Not affiliated with Apple. Use at your own risk. Always have multiple backups.
