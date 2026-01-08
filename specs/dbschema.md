# Apple Photos Database Schema

This documents the SQLite tables in the Apple Photos library database (`Photos.sqlite`) that darwin-photos queries.

## Tables

### ZASSET

Main table storing photo and video assets.

| Column | Type | Description |
|--------|------|-------------|
| Z_PK | INTEGER | Primary key |
| ZUUID | TEXT | Unique identifier for the asset |
| ZFILENAME | TEXT | Current filename |
| ZDIRECTORY | TEXT | Directory path within library |
| ZWIDTH | INTEGER | Image/video width in pixels |
| ZHEIGHT | INTEGER | Image/video height in pixels |
| ZKIND | INTEGER | Asset type: 0 = photo, 1 = video |
| ZUNIFORMTYPEIDENTIFIER | TEXT | UTI (e.g., "public.jpeg", "public.heic") |
| ZFAVORITE | INTEGER | 1 if favorited, 0 otherwise |
| ZHIDDEN | INTEGER | 1 if hidden, 0 otherwise |
| ZTRASHEDSTATE | INTEGER | 0 or NULL = not trashed, non-zero = trashed |
| ZCLOUDLOCALSTATE | INTEGER | Cloud sync state |
| ZDATECREATED | REAL | Creation timestamp (Core Data epoch) |
| ZMODIFICATIONDATE | REAL | Modification timestamp (Core Data epoch) |
| ZLATITUDE | REAL | GPS latitude |
| ZLONGITUDE | REAL | GPS longitude |
| ZDURATION | REAL | Video duration in seconds |
| ZADDITIONALATTRIBUTES | INTEGER | FK to ZADDITIONALASSETATTRIBUTES.Z_PK |
| ZEXTENDEDATTRIBUTES | INTEGER | FK to ZEXTENDEDATTRIBUTES.Z_PK |
| ZMASTER | INTEGER | FK to ZCLOUDMASTER.Z_PK |

### ZADDITIONALASSETATTRIBUTES

Additional metadata for assets.

| Column | Type | Description |
|--------|------|-------------|
| Z_PK | INTEGER | Primary key |
| ZORIGINALFILENAME | TEXT | Original filename at import |
| ZORIGINALFILESIZE | INTEGER | Original file size in bytes |

### ZINTERNALRESOURCE

Resource entries representing different versions/sizes of assets.

| Column | Type | Description |
|--------|------|-------------|
| ZASSET | INTEGER | FK to ZASSET.Z_PK |
| ZRESOURCETYPE | INTEGER | 0 = original resource |
| ZLOCALAVAILABILITY | INTEGER | 1 = available locally, -1 = cloud only |
| ZDATALENGTH | INTEGER | File size in bytes |
| ZFINGERPRINT | TEXT | Content hash (empty for ghost entries) |

**Notes:**
- Ghost entries have NULL or empty ZFINGERPRINT and should be filtered out
- When multiple resources exist for an asset, select by largest ZDATALENGTH to get the original

### ZEXTENDEDATTRIBUTES

EXIF and camera metadata.

| Column | Type | Description |
|--------|------|-------------|
| Z_PK | INTEGER | Primary key |
| ZCAMERAMAKE | TEXT | Camera manufacturer |
| ZCAMERAMODEL | TEXT | Camera model |
| ZLENSMODEL | TEXT | Lens model |
| ZISO | INTEGER | ISO sensitivity |
| ZAPERTURE | REAL | Aperture (f-number) |
| ZSHUTTERSPEED | REAL | Shutter speed |
| ZFOCALLENGTH | REAL | Focal length in mm |

### ZGENERICALBUM

Albums and folders.

| Column | Type | Description |
|--------|------|-------------|
| Z_PK | INTEGER | Primary key |
| ZUUID | TEXT | Unique identifier |
| ZTITLE | TEXT | Album name |
| ZKIND | INTEGER | 2 = regular user album |

### Z_*ASSETS (Dynamic Table)

Album-to-asset relationship table. The table name varies by macOS version (e.g., `Z_30ASSETS`, `Z_28ASSETS`).

| Column | Type | Description |
|--------|------|-------------|
| Z_*ALBUMS | INTEGER | FK to ZGENERICALBUM.Z_PK (column name varies, e.g., Z_30ALBUMS) |
| Z_3ASSETS | INTEGER | FK to ZASSET.Z_PK |

**Discovery:** Query `sqlite_master` for tables matching pattern `Z_[0-9]*ASSETS` to find the correct table name.

### ZCLOUDMASTER

iCloud sync metadata.

| Column | Type | Description |
|--------|------|-------------|
| Z_PK | INTEGER | Primary key |
| ZCLOUDMASTERGUID | TEXT | CloudKit record identifier |

## Timestamps

Apple Photos uses Core Data timestamps: seconds since January 1, 2001 (Core Data epoch).

```go
var coreDataEpoch = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
```

To convert to standard time: `coreDataEpoch.Add(time.Duration(seconds * float64(time.Second)))`

## Common Query Patterns

### Get asset with largest original resource

```sql
SELECT r.ZDATALENGTH, r.ZLOCALAVAILABILITY
FROM ZINTERNALRESOURCE r
WHERE r.ZASSET = ? AND r.ZRESOURCETYPE = 0
  AND r.ZFINGERPRINT IS NOT NULL AND r.ZFINGERPRINT != ''
ORDER BY r.ZDATALENGTH DESC
LIMIT 1
```

### Find album relationship table

```sql
SELECT name FROM sqlite_master
WHERE type='table' AND name GLOB 'Z_[0-9]*ASSETS'
LIMIT 1
```

### Filter local-only assets

```sql
WHERE (SELECT r.ZLOCALAVAILABILITY FROM ZINTERNALRESOURCE r
       WHERE r.ZASSET = a.Z_PK AND r.ZRESOURCETYPE = 0
         AND r.ZFINGERPRINT IS NOT NULL AND r.ZFINGERPRINT != ''
       ORDER BY r.ZDATALENGTH DESC LIMIT 1) = 1
```

### Filter cloud-only assets

```sql
WHERE (SELECT r.ZLOCALAVAILABILITY FROM ZINTERNALRESOURCE r
       WHERE r.ZASSET = a.Z_PK AND r.ZRESOURCETYPE = 0
         AND r.ZFINGERPRINT IS NOT NULL AND r.ZFINGERPRINT != ''
       ORDER BY r.ZDATALENGTH DESC LIMIT 1) = -1
```
