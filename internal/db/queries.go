package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/cleanexit0/darwin-photos/internal/models"
)

// Core Data epoch: January 1, 2001
var coreDataEpoch = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

// fromCoreDataTimestamp converts Core Data timestamp to Go time
func fromCoreDataTimestamp(seconds float64) time.Time {
	if seconds == 0 {
		return time.Time{}
	}
	return coreDataEpoch.Add(time.Duration(seconds * float64(time.Second)))
}

// parseTimestamp handles both time.Time and float64 (Core Data) timestamps
func parseTimestamp(v interface{}) time.Time {
	if v == nil {
		return time.Time{}
	}
	switch t := v.(type) {
	case time.Time:
		return t
	case float64:
		return fromCoreDataTimestamp(t)
	case int64:
		return fromCoreDataTimestamp(float64(t))
	default:
		return time.Time{}
	}
}

// ListOptions configures the list query
type ListOptions struct {
	Limit          int
	Offset         int
	LocalOnly      bool
	CloudOnly      bool
	PhotosOnly     bool
	VideosOnly     bool
	IncludeTrashed bool
	SortBy         string // "date", "name", "size"
	SortDescending bool
	StartDate      time.Time // Include photos on or after this date (inclusive)
	EndDate        time.Time // Include photos on or before this date (inclusive)
	AlbumName      string    // Filter by album name (exact match, case-sensitive)
}

// getAlbumRelationNumber discovers the dynamic album-asset relationship number.
// The Photos database uses tables like Z_30ASSETS with columns Z_30ALBUMS and Z_3ASSETS.
// The number (e.g., 30) varies by macOS version, while Z_3ASSETS is constant.
// Returns -1 if the relationship table doesn't exist.
func getAlbumRelationNumber(db *sql.DB) (int, error) {
	var tableName string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name GLOB 'Z_[0-9]*ASSETS' LIMIT 1`).Scan(&tableName)
	if err != nil {
		if err == sql.ErrNoRows {
			return -1, nil
		}
		return -1, err
	}

	// Extract number from table name (e.g., Z_30ASSETS -> 30)
	numStr := strings.TrimPrefix(tableName, "Z_")
	numStr = strings.TrimSuffix(numStr, "ASSETS")

	var num int
	_, err = fmt.Sscanf(numStr, "%d", &num)
	if err != nil {
		return -1, fmt.Errorf("failed to parse album relation number from %q: %w", tableName, err)
	}

	return num, nil
}

// ListAssets returns a list of assets based on the options
func ListAssets(db *sql.DB, opts ListOptions) ([]*models.Asset, int, error) {
	// Discover album relationship number if filtering by album
	var albumRelNum int = -1
	if opts.AlbumName != "" {
		var err error
		albumRelNum, err = getAlbumRelationNumber(db)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to discover album table: %w", err)
		}
		if albumRelNum < 0 {
			return nil, 0, fmt.Errorf("album relationship table not found in database")
		}
	}

	// Build the query - use subquery to get the largest resource (original) per asset
	// Note: CAST timestamps AS REAL to prevent sqlite3 driver from auto-converting to time.Time
	query := `
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
`
	var args []interface{}

	// Add album JOINs if filtering by album
	if opts.AlbumName != "" {
		query += fmt.Sprintf(`JOIN Z_%dASSETS rel ON a.Z_PK = rel.Z_3ASSETS
JOIN ZGENERICALBUM alb ON rel.Z_%dALBUMS = alb.Z_PK
`, albumRelNum, albumRelNum)
	}

	query += `WHERE 1=1
`

	// Filter by album
	if opts.AlbumName != "" {
		query += " AND alb.ZTITLE = ? AND alb.ZKIND = 2"
		args = append(args, opts.AlbumName)
	}

	// Filter by trashed state
	if !opts.IncludeTrashed {
		query += " AND (a.ZTRASHEDSTATE = 0 OR a.ZTRASHEDSTATE IS NULL)"
	}

	// Filter by local/cloud availability (using subquery)
	// Exclude ghost entries by requiring ZFINGERPRINT (content hash) to exist.
	// Edge case: If an asset has ONLY ghost entries, the subquery returns NULL, causing
	// LocalAvailability to be 0 (unknown). This is intentional - such assets should be
	// treated as cloud-only since no valid local resource exists.
	if opts.LocalOnly {
		query += " AND (SELECT r.ZLOCALAVAILABILITY FROM ZINTERNALRESOURCE r WHERE r.ZASSET = a.Z_PK AND r.ZRESOURCETYPE = 0 AND r.ZFINGERPRINT IS NOT NULL AND r.ZFINGERPRINT != '' ORDER BY r.ZDATALENGTH DESC LIMIT 1) = 1"
	} else if opts.CloudOnly {
		query += " AND (SELECT r.ZLOCALAVAILABILITY FROM ZINTERNALRESOURCE r WHERE r.ZASSET = a.Z_PK AND r.ZRESOURCETYPE = 0 AND r.ZFINGERPRINT IS NOT NULL AND r.ZFINGERPRINT != '' ORDER BY r.ZDATALENGTH DESC LIMIT 1) = -1"
	}

	// Filter by type
	if opts.PhotosOnly {
		query += " AND a.ZKIND = 0"
	} else if opts.VideosOnly {
		query += " AND a.ZKIND = 1"
	}

	// Filter by date range (Core Data timestamps: seconds since Jan 1, 2001)
	if !opts.StartDate.IsZero() {
		startSeconds := opts.StartDate.Sub(coreDataEpoch).Seconds()
		query += " AND a.ZDATECREATED >= ?"
		args = append(args, startSeconds)
	}
	if !opts.EndDate.IsZero() {
		// Add 1 day minus 1 second to make end date inclusive (23:59:59)
		endOfDay := opts.EndDate.Add(24*time.Hour - time.Second)
		endSeconds := endOfDay.Sub(coreDataEpoch).Seconds()
		query += " AND a.ZDATECREATED <= ?"
		args = append(args, endSeconds)
	}

	// Sorting
	orderBy := "a.ZDATECREATED"
	switch opts.SortBy {
	case "name":
		orderBy = "COALESCE(aa.ZORIGINALFILENAME, a.ZFILENAME)"
	case "size":
		orderBy = "data_length"
	}
	if opts.SortDescending {
		query += " ORDER BY " + orderBy + " DESC"
	} else {
		query += " ORDER BY " + orderBy + " ASC"
	}

	// Pagination
	if opts.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, opts.Limit)
	}
	if opts.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, opts.Offset)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	var assets []*models.Asset
	for rows.Next() {
		a := &models.Asset{}
		var (
			dateCreated     interface{}
			dateModified    interface{}
			latitude        sql.NullFloat64
			longitude       sql.NullFloat64
			duration        sql.NullFloat64
			origFilename    sql.NullString
			origFileSize    sql.NullInt64
			localAvail      sql.NullInt64
			dataLength      sql.NullInt64
			width           sql.NullInt64
			height          sql.NullInt64
			kind            sql.NullInt64
			uniformTypeID   sql.NullString
			favorite        sql.NullInt64
			hidden          sql.NullInt64
			trashed         sql.NullInt64
			cloudLocalState sql.NullInt64
			directory       sql.NullString
		)

		err := rows.Scan(
			&a.PK,
			&a.UUID,
			&a.Filename,
			&directory,
			&width,
			&height,
			&kind,
			&uniformTypeID,
			&favorite,
			&hidden,
			&trashed,
			&cloudLocalState,
			&dateCreated,
			&dateModified,
			&latitude,
			&longitude,
			&duration,
			&origFilename,
			&origFileSize,
			&localAvail,
			&dataLength,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan error: %w", err)
		}

		// Map nullable fields
		a.Directory = directory.String
		a.Width = int(width.Int64)
		a.Height = int(height.Int64)
		a.Kind = models.AssetKind(kind.Int64)
		a.UniformTypeID = uniformTypeID.String
		a.Favorite = favorite.Int64 == 1
		a.Hidden = hidden.Int64 == 1
		a.Trashed = trashed.Int64 != 0
		a.CloudLocalState = int(cloudLocalState.Int64)
		a.DateCreated = parseTimestamp(dateCreated)
		a.DateModified = parseTimestamp(dateModified)
		a.Latitude = latitude.Float64
		a.Longitude = longitude.Float64
		a.Duration = duration.Float64
		a.OriginalFilename = origFilename.String
		a.FileSize = dataLength.Int64
		if a.FileSize == 0 {
			a.FileSize = origFileSize.Int64
		}
		a.LocalAvailability = models.LocalAvailability(localAvail.Int64)

		assets = append(assets, a)
	}

	// Get total count (without pagination)
	countQuery := `SELECT COUNT(*) FROM ZASSET a `
	var countArgs []interface{}

	// Add album JOINs to count query if filtering by album
	if opts.AlbumName != "" {
		countQuery += fmt.Sprintf(`JOIN Z_%dASSETS rel ON a.Z_PK = rel.Z_3ASSETS
JOIN ZGENERICALBUM alb ON rel.Z_%dALBUMS = alb.Z_PK `, albumRelNum, albumRelNum)
	}

	countQuery += `WHERE 1=1`

	// Filter by album in count
	if opts.AlbumName != "" {
		countQuery += " AND alb.ZTITLE = ? AND alb.ZKIND = 2"
		countArgs = append(countArgs, opts.AlbumName)
	}
	if !opts.IncludeTrashed {
		countQuery += " AND (a.ZTRASHEDSTATE = 0 OR a.ZTRASHEDSTATE IS NULL)"
	}
	if opts.LocalOnly {
		countQuery += " AND (SELECT r.ZLOCALAVAILABILITY FROM ZINTERNALRESOURCE r WHERE r.ZASSET = a.Z_PK AND r.ZRESOURCETYPE = 0 AND r.ZFINGERPRINT IS NOT NULL AND r.ZFINGERPRINT != '' ORDER BY r.ZDATALENGTH DESC LIMIT 1) = 1"
	} else if opts.CloudOnly {
		countQuery += " AND (SELECT r.ZLOCALAVAILABILITY FROM ZINTERNALRESOURCE r WHERE r.ZASSET = a.Z_PK AND r.ZRESOURCETYPE = 0 AND r.ZFINGERPRINT IS NOT NULL AND r.ZFINGERPRINT != '' ORDER BY r.ZDATALENGTH DESC LIMIT 1) = -1"
	}
	if opts.PhotosOnly {
		countQuery += " AND a.ZKIND = 0"
	} else if opts.VideosOnly {
		countQuery += " AND a.ZKIND = 1"
	}
	if !opts.StartDate.IsZero() {
		startSeconds := opts.StartDate.Sub(coreDataEpoch).Seconds()
		countQuery += " AND a.ZDATECREATED >= ?"
		countArgs = append(countArgs, startSeconds)
	}
	if !opts.EndDate.IsZero() {
		endOfDay := opts.EndDate.Add(24*time.Hour - time.Second)
		endSeconds := endOfDay.Sub(coreDataEpoch).Seconds()
		countQuery += " AND a.ZDATECREATED <= ?"
		countArgs = append(countArgs, endSeconds)
	}

	var total int
	db.QueryRow(countQuery, countArgs...).Scan(&total)

	return assets, total, nil
}

// GetAssetByUUID returns a single asset by UUID
func GetAssetByUUID(db *sql.DB, uuid string) (*models.Asset, error) {
	// Note: CAST timestamps AS REAL to prevent sqlite3 driver from auto-converting to time.Time
	query := `
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
WHERE a.ZUUID = ?
LIMIT 1
`
	row := db.QueryRow(query, uuid)

	a := &models.Asset{}
	var (
		dateCreated     interface{}
		dateModified    interface{}
		latitude        sql.NullFloat64
		longitude       sql.NullFloat64
		duration        sql.NullFloat64
		origFilename    sql.NullString
		origFileSize    sql.NullInt64
		localAvail      sql.NullInt64
		dataLength      sql.NullInt64
		width           sql.NullInt64
		height          sql.NullInt64
		kind            sql.NullInt64
		uniformTypeID   sql.NullString
		favorite        sql.NullInt64
		hidden          sql.NullInt64
		trashed         sql.NullInt64
		cloudLocalState sql.NullInt64
		directory       sql.NullString
	)

	err := row.Scan(
		&a.PK,
		&a.UUID,
		&a.Filename,
		&directory,
		&width,
		&height,
		&kind,
		&uniformTypeID,
		&favorite,
		&hidden,
		&trashed,
		&cloudLocalState,
		&dateCreated,
		&dateModified,
		&latitude,
		&longitude,
		&duration,
		&origFilename,
		&origFileSize,
		&localAvail,
		&dataLength,
	)
	if err != nil {
		return nil, err
	}

	// Map nullable fields
	a.Directory = directory.String
	a.Width = int(width.Int64)
	a.Height = int(height.Int64)
	a.Kind = models.AssetKind(kind.Int64)
	a.UniformTypeID = uniformTypeID.String
	a.Favorite = favorite.Int64 == 1
	a.Hidden = hidden.Int64 == 1
	a.Trashed = trashed.Int64 != 0
	a.CloudLocalState = int(cloudLocalState.Int64)
	a.DateCreated = parseTimestamp(dateCreated)
	a.DateModified = parseTimestamp(dateModified)
	a.Latitude = latitude.Float64
	a.Longitude = longitude.Float64
	a.Duration = duration.Float64
	a.OriginalFilename = origFilename.String
	a.FileSize = dataLength.Int64
	if a.FileSize == 0 {
		a.FileSize = origFileSize.Int64
	}
	a.LocalAvailability = models.LocalAvailability(localAvail.Int64)

	return a, nil
}

// GetExtendedAttributes returns EXIF data for an asset
func GetExtendedAttributes(db *sql.DB, assetPK int64) (*models.ExtendedAttributes, error) {
	query := `
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
`
	row := db.QueryRow(query, assetPK)

	ext := &models.ExtendedAttributes{}
	var (
		cameraMake   sql.NullString
		cameraModel  sql.NullString
		lensModel    sql.NullString
		iso          sql.NullInt64
		aperture     sql.NullFloat64
		shutterSpeed sql.NullFloat64
		focalLength  sql.NullFloat64
	)

	err := row.Scan(
		&cameraMake,
		&cameraModel,
		&lensModel,
		&iso,
		&aperture,
		&shutterSpeed,
		&focalLength,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // No extended attributes
		}
		return nil, err
	}

	ext.CameraMake = cameraMake.String
	ext.CameraModel = cameraModel.String
	ext.LensModel = lensModel.String
	ext.ISO = int(iso.Int64)
	ext.Aperture = aperture.Float64
	ext.ShutterSpeed = shutterSpeed.Float64
	ext.FocalLength = focalLength.Float64

	return ext, nil
}

// GetAlbumsForAsset returns albums that contain the given asset
func GetAlbumsForAsset(db *sql.DB, assetPK int64) ([]*models.Album, error) {
	albumRelNum, err := getAlbumRelationNumber(db)
	if err != nil || albumRelNum < 0 {
		return nil, nil
	}

	query := fmt.Sprintf(`
SELECT alb.Z_PK, alb.ZUUID, alb.ZTITLE
FROM ZGENERICALBUM alb
JOIN Z_%dASSETS rel ON alb.Z_PK = rel.Z_%dALBUMS
WHERE rel.Z_3ASSETS = ?
  AND alb.ZKIND = 2
  AND alb.ZTITLE IS NOT NULL
`, albumRelNum, albumRelNum)
	rows, err := db.Query(query, assetPK)
	if err != nil {
		return nil, nil
	}
	defer rows.Close()

	var albums []*models.Album
	for rows.Next() {
		alb := &models.Album{}
		var title sql.NullString
		var uuid sql.NullString
		if err := rows.Scan(&alb.PK, &uuid, &title); err != nil {
			continue
		}
		alb.UUID = uuid.String
		alb.Title = title.String
		albums = append(albums, alb)
	}

	return albums, nil
}

// sqlBatchSize is the maximum number of SQL variables per query batch.
// SQLite default limit is 999 (SQLITE_MAX_VARIABLE_NUMBER).
const sqlBatchSize = 500

// GetCloudMasterGUIDs returns CloudKit GUIDs for multiple asset UUIDs.
// Queries are batched to avoid exceeding SQLite's variable limit.
func GetCloudMasterGUIDs(db *sql.DB, uuids []string) (map[string]string, error) {
	if len(uuids) == 0 {
		return make(map[string]string), nil
	}

	result := make(map[string]string)

	// Process in batches to avoid "too many SQL variables" error
	for i := 0; i < len(uuids); i += sqlBatchSize {
		end := i + sqlBatchSize
		if end > len(uuids) {
			end = len(uuids)
		}
		batch := uuids[i:end]

		// Build placeholders for IN clause
		placeholders := make([]string, len(batch))
		args := make([]interface{}, len(batch))
		for j, uuid := range batch {
			placeholders[j] = "?"
			args[j] = uuid
		}

		query := fmt.Sprintf(`
SELECT a.ZUUID, cm.ZCLOUDMASTERGUID
FROM ZASSET a
JOIN ZCLOUDMASTER cm ON a.ZMASTER = cm.Z_PK
WHERE a.ZUUID IN (%s) AND cm.ZCLOUDMASTERGUID IS NOT NULL AND cm.ZCLOUDMASTERGUID != ''
`, strings.Join(placeholders, ", "))

		rows, err := db.Query(query, args...)
		if err != nil {
			return nil, err
		}

		for rows.Next() {
			var uuid, guid string
			if err := rows.Scan(&uuid, &guid); err != nil {
				rows.Close()
				return nil, err
			}
			result[uuid] = guid
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}

	return result, nil
}

// GetAssetSizes returns file sizes for multiple asset UUIDs.
// Returns a map of UUID -> fileSize and total bytes.
// Queries are batched to avoid exceeding SQLite's variable limit.
func GetAssetSizes(db *sql.DB, uuids []string) (map[string]int64, int64, error) {
	if len(uuids) == 0 {
		return make(map[string]int64), 0, nil
	}

	result := make(map[string]int64)
	var totalBytes int64

	// Process in batches to avoid "too many SQL variables" error
	for i := 0; i < len(uuids); i += sqlBatchSize {
		end := i + sqlBatchSize
		if end > len(uuids) {
			end = len(uuids)
		}
		batch := uuids[i:end]

		// Build placeholders for IN clause
		placeholders := make([]string, len(batch))
		args := make([]interface{}, len(batch))
		for j, uuid := range batch {
			placeholders[j] = "?"
			args[j] = uuid
		}

		query := fmt.Sprintf(`
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
WHERE a.ZUUID IN (%s)
`, strings.Join(placeholders, ", "))

		rows, err := db.Query(query, args...)
		if err != nil {
			return nil, 0, err
		}

		for rows.Next() {
			var uuid string
			var fileSize int64
			if err := rows.Scan(&uuid, &fileSize); err != nil {
				rows.Close()
				return nil, 0, err
			}
			result[uuid] = fileSize
			totalBytes += fileSize
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, 0, err
		}
		rows.Close()
	}

	return result, totalBytes, nil
}

// Stats holds aggregate statistics for the photo library
type Stats struct {
	TotalCount     int
	TotalSize      int64
	OriginalSize   int64 // Size of ZRESOURCETYPE = 0 only
	DerivativeSize int64 // Size of ZRESOURCETYPE > 0 (calculated as TotalSize - OriginalSize)
	LocalCacheSize int64 // Size of cloud-only type 3 resources (JPEG previews, not in iCloud)
	LocalCount     int
	LocalSize      int64
	CloudCount     int
	CloudSize      int64
	PhotoCount     int
	PhotoSize      int64
	VideoCount     int
	VideoSize      int64
}

// GetStats returns aggregate statistics for the photo library
func GetStats(db *sql.DB) (*Stats, error) {
	// Query aggregates count and size, grouped by local availability and asset kind
	// Uses the same filtering logic as ListAssets (excludes trashed, ghost entries)
	// Counts both total size (all resources) and original size (ZRESOURCETYPE = 0 only)
	query := `
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
`
	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("stats query failed: %w", err)
	}
	defer rows.Close()

	stats := &Stats{}
	for rows.Next() {
		var localAvail, kind int
		var count int
		var size, originalSize int64
		if err := rows.Scan(&localAvail, &kind, &count, &size, &originalSize); err != nil {
			return nil, fmt.Errorf("scan error: %w", err)
		}

		stats.TotalCount += count
		stats.TotalSize += size
		stats.OriginalSize += originalSize

		// Local availability: 1 = local, -1 = cloud-only, 0 = unknown (treat as cloud)
		if localAvail == 1 {
			stats.LocalCount += count
			stats.LocalSize += size
		} else {
			stats.CloudCount += count
			stats.CloudSize += size
		}

		// Kind: 0 = photo, 1 = video
		if kind == 0 {
			stats.PhotoCount += count
			stats.PhotoSize += size
		} else {
			stats.VideoCount += count
			stats.VideoSize += size
		}
	}

	// Calculate derivative size (total - originals)
	stats.DerivativeSize = stats.TotalSize - stats.OriginalSize

	// Calculate local cache size (large type 3 resources - high-res JPEG previews for cloud photos)
	// Type 3 resources >= 3 MB are high-resolution previews cached locally but not stored in iCloud
	// Smaller type 3 resources are stored in iCloud for quick access
	cacheQuery := `
SELECT COALESCE(SUM(ZDATALENGTH), 0)
FROM ZINTERNALRESOURCE
WHERE ZRESOURCETYPE = 3
  AND ZLOCALAVAILABILITY = -1
  AND ZDATALENGTH >= 3145728
  AND ZFINGERPRINT IS NOT NULL AND ZFINGERPRINT != ''
`
	if err := db.QueryRow(cacheQuery).Scan(&stats.LocalCacheSize); err != nil {
		return nil, fmt.Errorf("local cache query failed: %w", err)
	}

	return stats, rows.Err()
}

// GetAssetDates returns creation and modification dates for multiple asset UUIDs.
// Returns a map of UUID -> {DateCreated, DateModified}.
// Queries are batched to avoid exceeding SQLite's variable limit.
func GetAssetDates(db *sql.DB, uuids []string) (map[string][2]time.Time, error) {
	if len(uuids) == 0 {
		return make(map[string][2]time.Time), nil
	}

	result := make(map[string][2]time.Time)

	// Process in batches to avoid "too many SQL variables" error
	for i := 0; i < len(uuids); i += sqlBatchSize {
		end := i + sqlBatchSize
		if end > len(uuids) {
			end = len(uuids)
		}
		batch := uuids[i:end]

		// Build placeholders for IN clause
		placeholders := make([]string, len(batch))
		args := make([]interface{}, len(batch))
		for j, uuid := range batch {
			placeholders[j] = "?"
			args[j] = uuid
		}

		query := fmt.Sprintf(`
SELECT ZUUID, CAST(ZDATECREATED AS REAL), CAST(ZMODIFICATIONDATE AS REAL)
FROM ZASSET
WHERE ZUUID IN (%s)
`, strings.Join(placeholders, ", "))

		rows, err := db.Query(query, args...)
		if err != nil {
			return nil, err
		}

		for rows.Next() {
			var uuid string
			var dateCreated, dateModified interface{}
			if err := rows.Scan(&uuid, &dateCreated, &dateModified); err != nil {
				rows.Close()
				return nil, err
			}
			result[uuid] = [2]time.Time{parseTimestamp(dateCreated), parseTimestamp(dateModified)}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}

	return result, nil
}

// ListAlbums returns all user-created albums
func ListAlbums(db *sql.DB) ([]*models.Album, error) {
	query := `
SELECT Z_PK, ZUUID, ZTITLE
FROM ZGENERICALBUM
WHERE ZKIND = 2 AND ZTITLE IS NOT NULL
ORDER BY ZTITLE
`
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var albums []*models.Album
	for rows.Next() {
		alb := &models.Album{}
		var uuid sql.NullString
		var title sql.NullString
		if err := rows.Scan(&alb.PK, &uuid, &title); err != nil {
			return nil, err
		}
		alb.UUID = uuid.String
		alb.Title = title.String
		albums = append(albums, alb)
	}

	return albums, rows.Err()
}
