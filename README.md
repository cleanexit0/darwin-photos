# darwin-photos

A CLI tool to interact with your iCloud Photos library on macOS. It reads your local Photos.sqlite database to list photos, show metadata, and download cloud-only photos without re-authenticating with iCloud.

## Highlights

**Back up your iCloud Photos without filling your Mac's disk.**

The Photos app's "Download Originals" option stores everything locally — great for backups, but it can quickly consume your internal storage. With `darwin-photos backup`, you can keep Photos set to "Optimize Mac Storage" while saving your entire library to an external drive.

Under the hood, it downloads cloud-only photos the same way the iCloud web app does — just automated and scriptable

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
