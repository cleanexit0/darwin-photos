# 🦤 darwin-photos

> Back up iCloud Photos to external storage — without downloading everything to your Mac first.

## The problem

iCloud Photos' "Optimize Storage" saves disk space, but your full-resolution photos only exist in the cloud. Apple provides no easy way to back them up to external storage without first downloading everything to your Mac.

## The solution

**darwin-photos** backs up your entire iCloud Photos library to an external drive while keeping your Mac set to "Optimize Storage".

```
$ darwin-photos backup /Volumes/External/photos-backup
Scanning library... 12,847 photos (42.3 GB)
Scanning output directory... 8,291 already backed up
Local copies: 1,203 | Cloud downloads: 3,353

[████████████████░░░░] 67% | 28.4GB/42.3GB | 156 MB/s | ETA 2m
```

It reads your local Photos database and uses iCloud's web API to download originals — no re-authentication needed after initial cookie import.

## Installation

```bash
brew install cleanexit0/tap/darwin-photos
```

## Usage

```bash
darwin-photos --help
```

## Disclaimer

This is an **unofficial** tool and is not affiliated with, endorsed by, or associated with Apple Inc. in any way. Use at your own risk.
