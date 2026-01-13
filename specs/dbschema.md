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
| ZORIGINALFILESIZE | INTEGER | Original file size in bytes (fallback when ZINTERNALRESOURCE unavailable) |

### ZINTERNALRESOURCE

Resource entries representing different versions/sizes of assets.

| Column | Type | Description |
|--------|------|-------------|
| Z_PK | INTEGER | Primary key |
| ZASSET | INTEGER | FK to ZASSET.Z_PK |
| ZRESOURCETYPE | INTEGER | Resource type (see below) |
| ZLOCALAVAILABILITY | INTEGER | Availability status (see below) |
| ZDATALENGTH | INTEGER | File size in bytes |
| ZFINGERPRINT | TEXT | Content hash (empty for ghost entries) |

**ZRESOURCETYPE values:**
- `0` = Original resource (full-resolution original file)
- `3` = JPEG preview (high-res previews, used for local cache calculation)
- Other values = Various derivative/thumbnail resources

**ZLOCALAVAILABILITY values:**
- `1` = Available locally (file exists on disk)
- `-1` = Cloud only (not downloaded) OR for type 3 resources: exists locally but not synced to iCloud
- `0` or NULL = Unknown status

**Notes:**
- Ghost entries have NULL or empty ZFINGERPRINT and should always be filtered out
- When multiple resources exist for an asset, select by largest ZDATALENGTH to get the true original
- For type 3 resources (previews), ZLOCALAVAILABILITY = -1 indicates local cache not backed up to iCloud

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
| ZKIND | INTEGER | 2 = regular user album (other values are system albums) |

### Z_*ASSETS (Dynamic Table)

Album-to-asset relationship table. The table name varies by macOS version (e.g., `Z_30ASSETS`, `Z_28ASSETS`).

| Column | Type | Description |
|--------|------|-------------|
| Z_*ALBUMS | INTEGER | FK to ZGENERICALBUM.Z_PK (column name varies, e.g., Z_30ALBUMS) |
| Z_3ASSETS | INTEGER | FK to ZASSET.Z_PK (this column name is constant) |

**Discovery:** Query `sqlite_master` for tables matching pattern `Z_[0-9]*ASSETS` to find the correct table name.

### ZCLOUDMASTER

iCloud sync metadata.

| Column | Type | Description |
|--------|------|-------------|
| Z_PK | INTEGER | Primary key |
| ZCLOUDMASTERGUID | TEXT | CloudKit record identifier (used for iCloud downloads) |

## Timestamps

Apple Photos uses Core Data timestamps: seconds since January 1, 2001 (Core Data epoch).

```go
var coreDataEpoch = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
```

To convert to standard time: `coreDataEpoch.Add(time.Duration(seconds * float64(time.Second)))`

**Important:** When reading timestamps, cast to REAL to prevent the SQLite driver from auto-converting:
```sql
CAST(a.ZDATECREATED AS REAL)
```

## CLI Query Reference

This section documents all SQL queries used by darwin-photos CLI commands.

### Discover Album Relationship Table

**Used by:** `ls --album`, `cat` (to show album membership)
**Location:** `internal/db/queries.go:62`

```sql
SELECT name FROM sqlite_master
WHERE type='table' AND name GLOB 'Z_[0-9]*ASSETS'
LIMIT 1
```

Extracts the number from the table name (e.g., `Z_30ASSETS` → 30) to construct dynamic JOINs.

### List Assets (Main Query)

**Used by:** `ls` command
**Location:** `internal/db/queries.go:100-209`

```sql
SELECT
    a.Z_PK,
    a.ZUUID,
    a.ZFILENAME,
    a.ZDIRECTORY,
    a.ZWIDTH,
    a.ZHEIGHT,
    a.ZKIND,
    a.ZUNIFORMTYPEIDENTIFIER,
    a.ZFAVORITE,
    a.ZHIDDEN,
    a.ZTRASHEDSTATE,
    a.ZCLOUDLOCALSTATE,
    CAST(a.ZDATECREATED AS REAL),
    CAST(a.ZMODIFICATIONDATE AS REAL),
    a.ZLATITUDE,
    a.ZLONGITUDE,
    a.ZDURATION,
    aa.ZORIGINALFILENAME,
    aa.ZORIGINALFILESIZE,
    (SELECT r.ZLOCALAVAILABILITY FROM ZINTERNALRESOURCE r
     WHERE r.ZASSET = a.Z_PK AND r.ZRESOURCETYPE = 0
       AND r.ZFINGERPRINT IS NOT NULL AND r.ZFINGERPRINT != ''
     ORDER BY r.ZDATALENGTH DESC LIMIT 1) as local_avail,
    (SELECT r.ZDATALENGTH FROM ZINTERNALRESOURCE r
     WHERE r.ZASSET = a.Z_PK AND r.ZRESOURCETYPE = 0
       AND r.ZFINGERPRINT IS NOT NULL AND r.ZFINGERPRINT != ''
     ORDER BY r.ZDATALENGTH DESC LIMIT 1) as data_length
FROM ZASSET a
LEFT JOIN ZADDITIONALASSETATTRIBUTES aa ON a.ZADDITIONALATTRIBUTES = aa.Z_PK
-- Dynamic filters added below
```

**Dynamic filters (appended based on CLI flags):**

| Flag | Filter |
|------|--------|
| `--album NAME` | `JOIN Z_{N}ASSETS rel ON a.Z_PK = rel.Z_3ASSETS JOIN ZGENERICALBUM alb ON rel.Z_{N}ALBUMS = alb.Z_PK ... AND alb.ZTITLE = ? AND alb.ZKIND = 2` |
| (default) | `AND (a.ZTRASHEDSTATE = 0 OR a.ZTRASHEDSTATE IS NULL)` |
| `--trashed` | (omits trashed filter) |
| `--local` | `AND (subquery for local_avail) = 1` |
| `--cloud` | `AND (subquery for local_avail) = -1` |
| `--photos` | `AND a.ZKIND = 0` |
| `--videos` | `AND a.ZKIND = 1` |
| `--start DATE` | `AND a.ZDATECREATED >= ?` (converted to Core Data timestamp) |
| `--end DATE` | `AND a.ZDATECREATED <= ?` (end of day, inclusive) |
| `--sort date\|name\|size` | `ORDER BY a.ZDATECREATED / COALESCE(aa.ZORIGINALFILENAME, a.ZFILENAME) / data_length` |
| `--desc` | `DESC` (default is ASC) |
| `--limit N` | `LIMIT ?` |
| `--offset N` | `OFFSET ?` |

**Count query:** Same structure without SELECT columns, pagination, or ORDER BY, wrapped in `SELECT COUNT(*)`.

### Get Asset by UUID

**Used by:** `cat` command
**Location:** `internal/db/queries.go:344-377`

Same as List Assets query but with `WHERE a.ZUUID = ? LIMIT 1`.

### Get Extended Attributes (EXIF)

**Used by:** `cat` command
**Location:** `internal/db/queries.go:456-468`

```sql
SELECT
    e.ZCAMERAMAKE,
    e.ZCAMERAMODEL,
    e.ZLENSMODEL,
    e.ZISO,
    e.ZAPERTURE,
    e.ZSHUTTERSPEED,
    e.ZFOCALLENGTH
FROM ZASSET a
JOIN ZEXTENDEDATTRIBUTES e ON a.ZEXTENDEDATTRIBUTES = e.Z_PK
WHERE a.Z_PK = ?
```

### Get Albums for Asset

**Used by:** `cat` command
**Location:** `internal/db/queries.go:516-523`

```sql
SELECT alb.Z_PK, alb.ZUUID, alb.ZTITLE
FROM ZGENERICALBUM alb
JOIN Z_{N}ASSETS rel ON alb.Z_PK = rel.Z_{N}ALBUMS
WHERE rel.Z_3ASSETS = ?
  AND alb.ZKIND = 2
  AND alb.ZTITLE IS NOT NULL
```

### Get CloudKit GUIDs (Batched)

**Used by:** `export`, `backup` commands (for iCloud downloads)
**Location:** `internal/db/queries.go:575-580`
**Batch size:** 500 UUIDs per query

```sql
SELECT a.ZUUID, cm.ZCLOUDMASTERGUID
FROM ZASSET a
JOIN ZCLOUDMASTER cm ON a.ZMASTER = cm.Z_PK
WHERE a.ZUUID IN (?, ?, ...)
  AND cm.ZCLOUDMASTERGUID IS NOT NULL
  AND cm.ZCLOUDMASTERGUID != ''
```

### Get Asset Sizes (Batched)

**Used by:** `export`, `backup` commands (for progress bars)
**Location:** `internal/db/queries.go:632-645`
**Batch size:** 500 UUIDs per query

```sql
SELECT a.ZUUID,
       COALESCE(
           (SELECT r.ZDATALENGTH FROM ZINTERNALRESOURCE r
            WHERE r.ZASSET = a.Z_PK AND r.ZRESOURCETYPE = 0
              AND r.ZFINGERPRINT IS NOT NULL AND r.ZFINGERPRINT != ''
            ORDER BY r.ZDATALENGTH DESC LIMIT 1),
           aa.ZORIGINALFILESIZE,
           0
       ) as file_size
FROM ZASSET a
LEFT JOIN ZADDITIONALASSETATTRIBUTES aa ON a.ZADDITIONALATTRIBUTES = aa.Z_PK
WHERE a.ZUUID IN (?, ?, ...)
```

### Get Stats (Aggregates)

**Used by:** `stats` command
**Location:** `internal/db/queries.go:694-724`

```sql
SELECT
    COALESCE((SELECT r.ZLOCALAVAILABILITY FROM ZINTERNALRESOURCE r
     WHERE r.ZASSET = a.Z_PK AND r.ZRESOURCETYPE = 0
       AND r.ZFINGERPRINT IS NOT NULL AND r.ZFINGERPRINT != ''
     ORDER BY r.ZDATALENGTH DESC LIMIT 1), 0) as local_avail,
    a.ZKIND,
    COUNT(*) as cnt,
    COALESCE(SUM(
        COALESCE(
            (SELECT SUM(r.ZDATALENGTH) FROM ZINTERNALRESOURCE r
             WHERE r.ZASSET = a.Z_PK
               AND r.ZFINGERPRINT IS NOT NULL AND r.ZFINGERPRINT != ''),
            aa.ZORIGINALFILESIZE,
            0
        )
    ), 0) as total_size,
    COALESCE(SUM(
        COALESCE(
            (SELECT SUM(r.ZDATALENGTH) FROM ZINTERNALRESOURCE r
             WHERE r.ZASSET = a.Z_PK AND r.ZRESOURCETYPE = 0
               AND r.ZFINGERPRINT IS NOT NULL AND r.ZFINGERPRINT != ''),
            aa.ZORIGINALFILESIZE,
            0
        )
    ), 0) as original_size
FROM ZASSET a
LEFT JOIN ZADDITIONALASSETATTRIBUTES aa ON a.ZADDITIONALATTRIBUTES = aa.Z_PK
WHERE (a.ZTRASHEDSTATE = 0 OR a.ZTRASHEDSTATE IS NULL)
GROUP BY local_avail, a.ZKIND
```

Returns rows grouped by (local_avail, kind) which are aggregated in Go to compute:
- TotalCount, TotalSize, OriginalSize
- LocalCount, LocalSize (where local_avail = 1)
- CloudCount, CloudSize (where local_avail != 1)
- PhotoCount, PhotoSize (where kind = 0)
- VideoCount, VideoSize (where kind = 1)
- DerivativeSize = TotalSize - OriginalSize

### Get Local Cache Size

**Used by:** `stats` command
**Location:** `internal/db/queries.go:769-776`

```sql
SELECT COALESCE(SUM(ZDATALENGTH), 0)
FROM ZINTERNALRESOURCE
WHERE ZRESOURCETYPE = 3
  AND ZLOCALAVAILABILITY = -1
  AND ZDATALENGTH >= 3145728
  AND ZFINGERPRINT IS NOT NULL AND ZFINGERPRINT != ''
```

Calculates size of high-res JPEG previews (>= 3 MB) that are cached locally but not synced to iCloud. These are type 3 resources with ZLOCALAVAILABILITY = -1 (meaning "local but not in iCloud" for derivative resources).

### Get Asset Dates (Batched)

**Used by:** `export`, `backup` commands (to preserve original timestamps)
**Location:** `internal/db/queries.go:810-814`
**Batch size:** 500 UUIDs per query

```sql
SELECT ZUUID, CAST(ZDATECREATED AS REAL), CAST(ZMODIFICATIONDATE AS REAL)
FROM ZASSET
WHERE ZUUID IN (?, ?, ...)
```

### List Albums

**Used by:** `albums` command
**Location:** `internal/db/queries.go:842-847`

```sql
SELECT Z_PK, ZUUID, ZTITLE
FROM ZGENERICALBUM
WHERE ZKIND = 2 AND ZTITLE IS NOT NULL
ORDER BY ZTITLE
```

## Query Patterns & Optimizations

### Filtering Ghost Entries

Ghost entries in ZINTERNALRESOURCE have NULL or empty ZFINGERPRINT. Always filter them:

```sql
AND r.ZFINGERPRINT IS NOT NULL AND r.ZFINGERPRINT != ''
```

### Getting the Original Resource

When an asset has multiple resources (original, derivatives, previews), get the original with the largest size:

```sql
SELECT r.ZDATALENGTH, r.ZLOCALAVAILABILITY
FROM ZINTERNALRESOURCE r
WHERE r.ZASSET = ? AND r.ZRESOURCETYPE = 0
  AND r.ZFINGERPRINT IS NOT NULL AND r.ZFINGERPRINT != ''
ORDER BY r.ZDATALENGTH DESC
LIMIT 1
```

**Why ORDER BY ZDATALENGTH DESC?** Some assets have multiple type-0 resources (e.g., edited versions). The largest is typically the true original.

### Correlated Subquery Pattern

The codebase uses correlated subqueries to fetch resource data inline:

```sql
SELECT
    ...,
    (SELECT r.ZLOCALAVAILABILITY FROM ZINTERNALRESOURCE r
     WHERE r.ZASSET = a.Z_PK AND r.ZRESOURCETYPE = 0
       AND r.ZFINGERPRINT IS NOT NULL AND r.ZFINGERPRINT != ''
     ORDER BY r.ZDATALENGTH DESC LIMIT 1) as local_avail
FROM ZASSET a
```

**Trade-offs:**
- Pros: Clean, readable, avoids complex JOINs with grouping
- Cons: Subquery executes per row (O(n) subqueries)
- Performance: Acceptable because Apple's indexes on ZINTERNALRESOURCE(ZASSET, ZRESOURCETYPE) make subqueries fast

**Alternative considered:** LEFT JOIN with ROW_NUMBER() window function. Not used because SQLite's window function support varies by version, and the subquery approach is more portable.

### Batching for Large IN Clauses

SQLite has a default limit of 999 variables (SQLITE_MAX_VARIABLE_NUMBER). Queries with many UUIDs are batched:

```go
const sqlBatchSize = 500

for i := 0; i < len(uuids); i += sqlBatchSize {
    batch := uuids[i:min(i+sqlBatchSize, len(uuids))]
    // Execute query with batch
}
```

### File Size Fallback Chain

File size uses a fallback chain for reliability:

```sql
COALESCE(
    (SELECT r.ZDATALENGTH FROM ZINTERNALRESOURCE r WHERE ...),
    aa.ZORIGINALFILESIZE,
    0
)
```

1. First: ZINTERNALRESOURCE.ZDATALENGTH (most accurate)
2. Fallback: ZADDITIONALASSETATTRIBUTES.ZORIGINALFILESIZE
3. Default: 0

### Timestamp Handling

Always cast timestamps to REAL to prevent SQLite driver auto-conversion issues:

```sql
CAST(a.ZDATECREATED AS REAL)
```

Then convert in Go using the Core Data epoch.

## Index Assumptions

These queries assume Apple's default indexes exist. Key indexes that affect performance:

- `ZASSET(ZUUID)` - UUID lookups
- `ZASSET(ZDATECREATED)` - Date sorting/filtering
- `ZINTERNALRESOURCE(ZASSET, ZRESOURCETYPE)` - Resource lookups
- `ZGENERICALBUM(ZKIND)` - Album filtering
- `Z_*ASSETS(Z_3ASSETS)` - Album membership lookups

Since this is Apple's database, we cannot add indexes. Query patterns are designed to work efficiently with the existing schema.
