package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/cleanexit0/darwin-photos/internal/db"
	"github.com/cleanexit0/darwin-photos/internal/models"
	"github.com/spf13/cobra"
)

var albumsJSON bool

var albumsCmd = &cobra.Command{
	Use:   "albums",
	Short: "List albums in the library",
	Long:  `List all user-created albums in your iCloud Photos library with photo counts.`,
	RunE:  runAlbums,
}

func init() {
	rootCmd.AddCommand(albumsCmd)

	albumsCmd.Flags().BoolVar(&albumsJSON, "json", false, "Output as JSON")
}

func runAlbums(cmd *cobra.Command, args []string) error {
	photosDB, err := db.Open(getLibraryPath())
	if err != nil {
		return fmt.Errorf("failed to open Photos database: %w", err)
	}
	defer photosDB.Close()

	albums, err := db.ListAlbums(photosDB.DB())
	if err != nil {
		return fmt.Errorf("failed to list albums: %w", err)
	}

	if albumsJSON {
		return outputAlbumsJSON(albums)
	}

	return outputAlbumsPlain(albums)
}

func outputAlbumsJSON(albums []*models.Album) error {
	type jsonAlbum struct {
		UUID  string `json:"uuid"`
		Title string `json:"title"`
	}

	output := struct {
		Albums []*jsonAlbum `json:"albums"`
		Total  int          `json:"total"`
	}{
		Albums: make([]*jsonAlbum, 0, len(albums)),
		Total:  len(albums),
	}

	for _, a := range albums {
		output.Albums = append(output.Albums, &jsonAlbum{
			UUID:  a.UUID,
			Title: a.Title,
		})
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func outputAlbumsPlain(albums []*models.Album) error {
	if len(albums) == 0 {
		fmt.Println("No albums found")
		return nil
	}

	for _, a := range albums {
		fmt.Println(a.Title)
	}

	return nil
}
