#ifndef PHOTOKIT_BRIDGE_H
#define PHOTOKIT_BRIDGE_H

// PKAuthStatus represents PHAuthorizationStatus values
typedef enum {
    PKAuthStatusNotDetermined = 0,
    PKAuthStatusRestricted = 1,
    PKAuthStatusDenied = 2,
    PKAuthStatusAuthorized = 3,
    PKAuthStatusLimited = 4
} PKAuthStatus;

// PKResult codes
typedef enum {
    PKResultSuccess = 0,
    PKResultErrorNotFound = 1,
    PKResultErrorDownloadFailed = 2,
    PKResultErrorWriteFailed = 3,
    PKResultErrorNotAuthorized = 4,
    PKResultErrorUnknown = 99
} PKResult;

// Check current Photos library authorization status
PKAuthStatus PKCheckAuthorization(void);

// Request Photos library authorization (blocks until user responds)
// Returns the resulting authorization status
PKAuthStatus PKRequestAuthorization(void);

// Download asset by local identifier to output path
// localIdentifier: PHAsset local identifier (format: "UUID/L0/001")
// outputPath: destination file path
// errorOut: if non-NULL, set to error message on failure (caller must free)
// Returns PKResultSuccess on success, error code otherwise
PKResult PKDownloadAsset(const char *localIdentifier, const char *outputPath, char **errorOut);

// Download asset for video (uses different export method)
PKResult PKDownloadVideoAsset(const char *localIdentifier, const char *outputPath, char **errorOut);

#endif // PHOTOKIT_BRIDGE_H
