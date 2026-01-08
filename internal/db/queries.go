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
}

// ListAssets returns a list of assets based on the options
func ListAssets(db *sql.DB, opts ListOptions) ([]*models.Asset, int, error) {
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
WHERE 1=1
`
	var args []interface{}

	// Filter by trashed state
	if !opts.IncludeTrashed {
		query += " AND (a.ZTRASHEDSTATE = 0 OR a.ZTRASHEDSTATE IS NULL)"
	}

	// Filter by local/cloud availability (using subquery)
	// Exclude ghost entries by requiring ZFINGERPRINT (content hash) to exist
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
	countQuery := `SELECT COUNT(*) FROM ZASSET a WHERE 1=1`
	var countArgs []interface{}
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
	// The relationship table name varies by macOS version
	// Try common table patterns
	tables := []string{
		"Z_26ASSETS", "Z_27ASSETS", "Z_28ASSETS", "Z_29ASSETS", "Z_30ASSETS",
		"Z_31ASSETS", "Z_32ASSETS", "Z_33ASSETS", "Z_34ASSETS",
	}

	var albums []*models.Album

	for _, tableName := range tables {
		// Check if table exists
		var exists int
		err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, tableName).Scan(&exists)
		if err != nil || exists == 0 {
			continue
		}

		// Try to find the correct column names
		query := `
SELECT alb.Z_PK, alb.ZUUID, alb.ZTITLE
FROM ZGENERICALBUM alb
JOIN ` + tableName + ` rel ON alb.Z_PK = rel.Z_26ALBUMS OR alb.Z_PK = rel.Z_27ALBUMS OR alb.Z_PK = rel.Z_28ALBUMS OR alb.Z_PK = rel.Z_29ALBUMS OR alb.Z_PK = rel.Z_30ALBUMS OR alb.Z_PK = rel.Z_31ALBUMS OR alb.Z_PK = rel.Z_32ALBUMS OR alb.Z_PK = rel.Z_33ALBUMS OR alb.Z_PK = rel.Z_34ALBUMS
WHERE (rel.Z_3ASSETS = ? OR rel.Z_26ASSETS = ? OR rel.Z_27ASSETS = ? OR rel.Z_28ASSETS = ?)
  AND alb.ZKIND = 2
  AND alb.ZTITLE IS NOT NULL
`
		rows, err := db.Query(query, assetPK, assetPK, assetPK, assetPK)
		if err != nil {
			continue
		}

		for rows.Next() {
			alb := &models.Album{}
			var title sql.NullString
			var uuid sql.NullString
			err := rows.Scan(&alb.PK, &uuid, &title)
			if err != nil {
				continue
			}
			alb.UUID = uuid.String
			alb.Title = title.String
			albums = append(albums, alb)
		}
		rows.Close()

		if len(albums) > 0 {
			break
		}
	}

	return albums, nil
}

// GetCloudMasterGUIDs returns CloudKit GUIDs for multiple asset UUIDs in a single query
func GetCloudMasterGUIDs(db *sql.DB, uuids []string) (map[string]string, error) {
	if len(uuids) == 0 {
		return make(map[string]string), nil
	}

	// Build placeholders for IN clause
	placeholders := make([]string, len(uuids))
	args := make([]interface{}, len(uuids))
	for i, uuid := range uuids {
		placeholders[i] = "?"
		args[i] = uuid
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
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var uuid, guid string
		if err := rows.Scan(&uuid, &guid); err != nil {
			return nil, err
		}
		result[uuid] = guid
	}
	return result, rows.Err()
}

// GetAssetSizes returns file sizes for multiple asset UUIDs
// Returns a map of UUID -> fileSize and total bytes
func GetAssetSizes(db *sql.DB, uuids []string) (map[string]int64, int64, error) {
	if len(uuids) == 0 {
		return make(map[string]int64), 0, nil
	}

	// Build placeholders for IN clause
	placeholders := make([]string, len(uuids))
	args := make([]interface{}, len(uuids))
	for i, uuid := range uuids {
		placeholders[i] = "?"
		args[i] = uuid
	}

	query := fmt.Sprintf(`
SELECT a.ZUUID,
       COALESCE(
           (SELECT r.ZDATALENGTH FROM ZINTERNALRESOURCE r
            WHERE r.ZASSET = a.Z_PK AND r.ZRESOURCETYPE = 0
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
	defer rows.Close()

	result := make(map[string]int64)
	var totalBytes int64
	for rows.Next() {
		var uuid string
		var fileSize int64
		if err := rows.Scan(&uuid, &fileSize); err != nil {
			return nil, 0, err
		}
		result[uuid] = fileSize
		totalBytes += fileSize
	}
	return result, totalBytes, rows.Err()
}
