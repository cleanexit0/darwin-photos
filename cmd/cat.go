package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cleanexit0/darwin-photos/internal/db"
	"github.com/cleanexit0/darwin-photos/internal/models"
	"github.com/spf13/cobra"
)

var (
	catJSON bool
)

var catCmd = &cobra.Command{
	Use:   "cat <uuid-or-filename>",
	Short: "Show detailed information about a photo",
	Long: `Display detailed metadata for a specific photo or video.

You can specify either:
- Full UUID (e.g., 4D507F5C-E9DE-41C4-80B5-008D3D4B352C)
- Partial UUID (e.g., 4D507F5C)
- Filename (e.g., IMG_1234.HEIC)`,
	Args: cobra.ExactArgs(1),
	RunE: runCat,
}

func init() {
	rootCmd.AddCommand(catCmd)

	catCmd.Flags().BoolVar(&catJSON, "json", false, "Output as JSON")
}

func runCat(cmd *cobra.Command, args []string) error {
	identifier := args[0]

	// Open database
	photosDB, err := db.Open(getLibraryPath())
	if err != nil {
		return fmt.Errorf("failed to open Photos database: %w", err)
	}
	defer photosDB.Close()

	// Find asset
	asset, err := db.GetAssetByUUID(photosDB.DB(), identifier)
	if err != nil {
		return fmt.Errorf("asset not found: %s", identifier)
	}

	// Get extended attributes (EXIF)
	ext, _ := db.GetExtendedAttributes(photosDB.DB(), asset.PK)

	// Get albums
	albums, _ := db.GetAlbumsForAsset(photosDB.DB(), asset.PK)

	if catJSON {
		return outputJSON(asset, ext, albums)
	}

	return outputFormatted(asset, ext, albums)
}

func outputJSON(asset *models.Asset, ext *models.ExtendedAttributes, albums []*models.Album) error {
	output := map[string]interface{}{
		"uuid":             asset.UUID,
		"filename":         asset.Filename,
		"originalFilename": asset.OriginalFilename,
		"type":             asset.TypeString(),
		"uniformTypeId":    asset.UniformTypeID,
		"dimensions": map[string]int{
			"width":  asset.Width,
			"height": asset.Height,
		},
		"fileSize":    asset.FileSize,
		"dateCreated": asset.DateCreated,
		"favorite":    asset.Favorite,
		"hidden":      asset.Hidden,
		"trashed":     asset.Trashed,
		"status":      asset.StatusString(),
		"localPath":   buildLocalPath(asset),
	}

	if asset.Latitude != 0 || asset.Longitude != 0 {
		output["location"] = map[string]float64{
			"latitude":  asset.Latitude,
			"longitude": asset.Longitude,
		}
	}

	if asset.Duration > 0 {
		output["duration"] = asset.Duration
	}

	if ext != nil {
		camera := map[string]interface{}{}
		if ext.CameraMake != "" {
			camera["make"] = ext.CameraMake
		}
		if ext.CameraModel != "" {
			camera["model"] = ext.CameraModel
		}
		if ext.LensModel != "" {
			camera["lens"] = ext.LensModel
		}
		if ext.ISO > 0 {
			camera["iso"] = ext.ISO
		}
		if ext.Aperture > 0 {
			camera["aperture"] = ext.Aperture
		}
		if ext.ShutterSpeed > 0 {
			camera["shutterSpeed"] = ext.ShutterSpeed
		}
		if ext.FocalLength > 0 {
			camera["focalLength"] = ext.FocalLength
		}
		if len(camera) > 0 {
			output["camera"] = camera
		}
	}

	if len(albums) > 0 {
		albumNames := make([]string, len(albums))
		for i, alb := range albums {
			albumNames[i] = alb.Title
		}
		output["albums"] = albumNames
	}

	enc := json.NewEncoder(nil)
	enc.SetIndent("", "  ")

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func outputFormatted(asset *models.Asset, ext *models.ExtendedAttributes, albums []*models.Album) error {
	fmt.Printf("uuid:      %s\n", asset.UUID)
	fmt.Printf("file:      %s\n", asset.OriginalFilename)
	if asset.Filename != asset.OriginalFilename {
		fmt.Printf("current:   %s\n", asset.Filename)
	}
	fmt.Printf("type:      %s (%s)\n", asset.TypeString(), asset.UniformTypeID)
	fmt.Printf("size:      %s (%d bytes)\n", formatBytes(asset.FileSize), asset.FileSize)
	if asset.Width > 0 && asset.Height > 0 {
		fmt.Printf("dims:      %dx%d\n", asset.Width, asset.Height)
	}
	if !asset.DateCreated.IsZero() {
		fmt.Printf("date:      %s\n", asset.DateCreated.Format("2006-01-02 15:04:05"))
	}
	if asset.Duration > 0 {
		fmt.Printf("duration:  %.1fs\n", asset.Duration)
	}
	fmt.Printf("status:    %s\n", asset.StatusString())
	fmt.Printf("path:      %s\n", buildLocalPath(asset))

	if asset.Latitude != 0 || asset.Longitude != 0 {
		fmt.Printf("location:  %.6f,%.6f\n", asset.Latitude, asset.Longitude)
	}

	if ext != nil {
		if ext.CameraMake != "" || ext.CameraModel != "" {
			camera := ext.CameraMake
			if ext.CameraModel != "" {
				if camera != "" {
					camera += " "
				}
				camera += ext.CameraModel
			}
			fmt.Printf("camera:    %s\n", camera)
		}
		if ext.LensModel != "" {
			fmt.Printf("lens:      %s\n", ext.LensModel)
		}
		if ext.ISO > 0 {
			fmt.Printf("iso:       %d\n", ext.ISO)
		}
		if ext.Aperture > 0 {
			fmt.Printf("aperture:  f/%.1f\n", ext.Aperture)
		}
		if ext.ShutterSpeed > 0 {
			if ext.ShutterSpeed < 1 {
				fmt.Printf("shutter:   1/%.0f\n", 1/ext.ShutterSpeed)
			} else {
				fmt.Printf("shutter:   %.1fs\n", ext.ShutterSpeed)
			}
		}
		if ext.FocalLength > 0 {
			fmt.Printf("focal:     %.1fmm\n", ext.FocalLength)
		}
	}

	if len(albums) > 0 {
		names := make([]string, len(albums))
		for i, alb := range albums {
			names[i] = alb.Title
		}
		fmt.Printf("albums:    %s\n", strings.Join(names, ", "))
	}

	var flags []string
	if asset.Favorite {
		flags = append(flags, "favorite")
	}
	if asset.Hidden {
		flags = append(flags, "hidden")
	}
	if asset.Trashed {
		flags = append(flags, "trashed")
	}
	if len(flags) > 0 {
		fmt.Printf("flags:     %s\n", strings.Join(flags, ", "))
	}

	return nil
}
