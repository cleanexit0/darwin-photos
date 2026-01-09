package icloud

import "errors"

// ErrSessionInvalid indicates the iCloud session has been invalidated server-side.
// This typically happens when the user signs out of iCloud or the session expires.
var ErrSessionInvalid = errors.New("iCloud session invalid")
