package models

import (
	"time"
)

// AssetKind represents photo vs video
type AssetKind int

const (
	AssetKindPhoto AssetKind = 0
	AssetKindVideo AssetKind = 1
)

// LocalAvailability represents download status
type LocalAvailability int

const (
	LocalAvailabilityUnknown LocalAvailability = 0
	LocalAvailabilityCloud   LocalAvailability = -1 // Cloud-only, not downloaded
	LocalAvailabilityLocal   LocalAvailability = 1  // Downloaded locally
)

// Asset represents a photo or video in the library
type Asset struct {
	PK                int64
	UUID              string
	Filename          string
	Directory         string // First char of UUID for path construction
	OriginalFilename  string
	UniformTypeID     string // e.g., "public.jpeg", "public.heic"
	Kind              AssetKind
	Width             int
	Height            int
	DateCreated       time.Time
	DateModified      time.Time
	Latitude          float64
	Longitude         float64
	Duration          float64 // For videos, in seconds
	Favorite          bool
	Hidden            bool
	Trashed           bool
	CloudLocalState   int
	LocalAvailability LocalAvailability
	FileSize          int64
}

// IsLocallyAvailable returns true if the original file exists on disk
func (a *Asset) IsLocallyAvailable() bool {
	return a.LocalAvailability == LocalAvailabilityLocal
}

// TypeString returns "Photo" or "Video"
func (a *Asset) TypeString() string {
	if a.Kind == AssetKindVideo {
		return "Video"
	}
	return "Photo"
}

// StatusString returns "Local" or "Cloud"
func (a *Asset) StatusString() string {
	if a.IsLocallyAvailable() {
		return "Local"
	}
	return "Cloud"
}

// ExtendedAttributes contains EXIF and camera metadata
type ExtendedAttributes struct {
	CameraMake   string
	CameraModel  string
	LensModel    string
	ISO          int
	Aperture     float64
	ShutterSpeed float64
	FocalLength  float64
}

// Album represents a photos album
type Album struct {
	PK    int64
	UUID  string
	Title string
}
