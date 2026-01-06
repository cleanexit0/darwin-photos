// Package photokit provides Go bindings to Apple's PhotoKit framework via cgo.
// This allows direct access to iCloud Photo Library for downloading cloud-only assets.
package photokit

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Photos -framework Foundation -framework AVFoundation
#include "bridge.h"
#include <stdlib.h>
*/
import "C"
import (
	"errors"
	"fmt"
	"unsafe"
)

// AuthStatus represents Photos library authorization status
type AuthStatus int

const (
	AuthStatusNotDetermined AuthStatus = 0
	AuthStatusRestricted    AuthStatus = 1
	AuthStatusDenied        AuthStatus = 2
	AuthStatusAuthorized    AuthStatus = 3
	AuthStatusLimited       AuthStatus = 4
)

// String returns human-readable authorization status
func (s AuthStatus) String() string {
	switch s {
	case AuthStatusNotDetermined:
		return "Not Determined"
	case AuthStatusRestricted:
		return "Restricted"
	case AuthStatusDenied:
		return "Denied"
	case AuthStatusAuthorized:
		return "Authorized"
	case AuthStatusLimited:
		return "Limited"
	default:
		return "Unknown"
	}
}

// IsAuthorized returns true if the status allows photo access
func (s AuthStatus) IsAuthorized() bool {
	return s == AuthStatusAuthorized || s == AuthStatusLimited
}

var (
	ErrNotFound       = errors.New("asset not found")
	ErrDownloadFailed = errors.New("download failed")
	ErrWriteFailed    = errors.New("failed to write file")
	ErrNotAuthorized  = errors.New("not authorized to access Photos")
	ErrUnknown        = errors.New("unknown error")
)

// CheckAuthorization returns the current Photos library authorization status
func CheckAuthorization() AuthStatus {
	status := C.PKCheckAuthorization()
	return AuthStatus(status)
}

// RequestAuthorization prompts the user for Photos library access if needed.
// This blocks until the user responds to the authorization dialog.
// Returns the resulting authorization status.
func RequestAuthorization() AuthStatus {
	status := C.PKRequestAuthorization()
	return AuthStatus(status)
}

// EnsureAuthorized checks authorization and requests it if needed.
// Returns nil if authorized, error otherwise.
func EnsureAuthorized() error {
	status := CheckAuthorization()

	switch status {
	case AuthStatusAuthorized, AuthStatusLimited:
		return nil
	case AuthStatusNotDetermined:
		// Request authorization
		newStatus := RequestAuthorization()
		if newStatus.IsAuthorized() {
			return nil
		}
		return fmt.Errorf("authorization denied (status: %s)", newStatus)
	case AuthStatusDenied:
		return errors.New("Photos access denied. Grant access in System Settings > Privacy & Security > Photos")
	case AuthStatusRestricted:
		return errors.New("Photos access restricted by system policy")
	default:
		return fmt.Errorf("unexpected authorization status: %d", status)
	}
}

// DownloadAsset downloads an image asset by its local identifier.
// localIdentifier should be in the format "UUID/L0/001".
// The file is written to outputPath.
func DownloadAsset(localIdentifier, outputPath string) error {
	cID := C.CString(localIdentifier)
	cPath := C.CString(outputPath)
	defer C.free(unsafe.Pointer(cID))
	defer C.free(unsafe.Pointer(cPath))

	var cError *C.char
	result := C.PKDownloadAsset(cID, cPath, &cError)

	if cError != nil {
		defer C.free(unsafe.Pointer(cError))
		errMsg := C.GoString(cError)
		return fmt.Errorf("%w: %s", resultToError(int(result)), errMsg)
	}

	if result != 0 {
		return resultToError(int(result))
	}

	return nil
}

// DownloadVideoAsset downloads a video asset by its local identifier.
// localIdentifier should be in the format "UUID/L0/001".
// The file is written to outputPath.
func DownloadVideoAsset(localIdentifier, outputPath string) error {
	cID := C.CString(localIdentifier)
	cPath := C.CString(outputPath)
	defer C.free(unsafe.Pointer(cID))
	defer C.free(unsafe.Pointer(cPath))

	var cError *C.char
	result := C.PKDownloadVideoAsset(cID, cPath, &cError)

	if cError != nil {
		defer C.free(unsafe.Pointer(cError))
		errMsg := C.GoString(cError)
		return fmt.Errorf("%w: %s", resultToError(int(result)), errMsg)
	}

	if result != 0 {
		return resultToError(int(result))
	}

	return nil
}

// UUIDToLocalIdentifier converts a Photos.sqlite UUID to a PhotoKit local identifier.
// The standard format is "UUID/L0/001".
func UUIDToLocalIdentifier(uuid string) string {
	return uuid + "/L0/001"
}

func resultToError(code int) error {
	switch code {
	case 0:
		return nil
	case 1:
		return ErrNotFound
	case 2:
		return ErrDownloadFailed
	case 3:
		return ErrWriteFailed
	case 4:
		return ErrNotAuthorized
	default:
		return ErrUnknown
	}
}
