# 🦤 darwin-photos

> Back up iCloud Photos to external storage — without bloating your Mac. Much faster. Progress bar included.

## The problem

> be me.
> 500GB of photos on iCloud.
> "Optimize Storage" on because MacBook only has 512GB.
> want a full copy on external drive. just to be safe.

Your options:

1. **"Download Originals"** — bloat your MacBook with 500GB of photos. runs in background. is it working? who knows. takes days. no progress bar. just vibes. then copy to external, turn Optimize back on, wait another week for re-upload.
2. **icloud.com** — manually select and download. flaky. slow. good luck.

## The solution

> read local Photos database.
> figure out what's on disk vs what's cloud-only.
> local files? copy directly to external. fast.
> cloud-only? grab your icloud.com cookies, download via API. progress bar. ETA. confidence.
> keep "Optimize Storage" on the whole time.
> profit.

```
$ darwin-photos backup /Volumes/External/photos-backup
Scanning library... 12,847 photos (42.3 GB)
Scanning output directory... 8,291 already backed up
Local copies: 1,203 | Cloud downloads: 3,353

[████████████████░░░░] 67% | 28.4GB/42.3GB | 156 MB/s | ETA 2m
```

Beyond backups, it's a full CLI for Apple Photos: list photos, filter by date/album, inspect metadata, and more.

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
