# darwin-photos

A CLI tool to interact with your iCloud Photos library on macOS. It reads your local Photos.sqlite database to list photos, show metadata, and download cloud-only photos without re-authenticating with iCloud.

## Highlights

- **Direct iCloud download** — Download photos directly from iCloud to any directory, bypassing the Photos app entirely

## Installation

```bash
brew install cleanexit0/tap/darwin-photos
```

## Usage

```bash
darwin-photos --help
```

## sync vs export

Both commands download cloud-only photos from iCloud, but they differ in where the photos end up:

| | `sync` | `export` |
|---|---|---|
| **Destination** | Local Photos library | Any directory (e.g., external drive) |
| **Uses local disk** | Yes | No |
| **Requires cookies** | No (uses PhotoKit) | Yes (direct iCloud download) |
| **Best for** | Making photos available in Photos app | Backup to external storage |

## Disclaimer

This is an **unofficial** tool and is not affiliated with, endorsed by, or associated with Apple Inc. in any way. Use at your own risk.
