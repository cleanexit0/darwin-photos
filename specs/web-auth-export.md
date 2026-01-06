# Plan: Implement iCloud Web API for Direct Downloads

## Goal
Add direct iCloud download capability, bypassing local Photos library cache. This enables backing up photos directly to external storage.

## Architecture
```
internal/icloud/
  client.go       # HTTP client with headers, cookie jar
  auth.go         # Login flow with SRP
  session.go      # Cookie persistence
  photos.go       # UUID → download URL translation
  download.go     # Stream files from URLs

cmd/
  export.go       # photoscli export ... (iCloud web download)
```

## Implementation Order

### 1. `internal/icloud/client.go` (~100 LOC)
- `Client` struct with httpClient, cookieJar, session tokens
- Required headers (Origin, Referer, User-Agent, X-Apple-Widget-Key)
- Base request methods

### 2. `internal/icloud/session.go` (~80 LOC)
- Save/load session to `~/.photoscli/session.json`
- Store cookies + auth tokens (dsid, sessionToken, scnt)
- Detect expired sessions

### 3. `internal/icloud/auth.go` (~200 LOC)
- `Login(username, password)` with SRP-6a protocol
- POST `/signin/init` → get salt, iterations
- Derive key with PBKDF2-SHA256
- POST `/signin/complete` → submit proof
- Handle 2FA trigger

### 4. `internal/icloud/twofactor.go` (~120 LOC)
- Detect `authType: "hsa2"` response
- Prompt user for 6-digit code (sent to trusted devices)
- POST `/verify/trusteddevice/securitycode`
- POST `/trust` to extend session

### 5. `internal/icloud/photos.go` (~150 LOC)
- `GetDownloadURL(uuid)` - query CloudKit for download URL
- POST to `/database/1/com.apple.photos.cloud/production/private/records/query`
- Map UUID to `CPLAsset` record → extract `resOriginalRes.downloadURL`

### 6. `internal/icloud/download.go` (~80 LOC)
- `DownloadPhoto(url, outputPath)` - stream to file
- Progress callback support
- Verify file size

### 7. `cmd/export.go` (~150 LOC)
- `photoscli export login` - authenticate, save session
- `photoscli export logout` - clear session
- `photoscli export <uuid> <output-dir>` - single photo
- `photoscli export --file <uuids.txt> <output-dir>` - from file
- `photoscli export - <output-dir>` - from stdin
- `photoscli export --all <output-dir>` - all cloud photos
- Reuse worker pool pattern from sync.go

## Dependencies to Add
```
github.com/1Password/srp  # SRP-6a implementation
golang.org/x/term         # Secure password input
```

## Key Endpoints
- `https://idmsa.apple.com/appleauth/auth/signin/init`
- `https://idmsa.apple.com/appleauth/auth/signin/complete`
- `https://idmsa.apple.com/appleauth/auth/verify/trusteddevice/securitycode`
- `https://p{XX}-ckdatabasews.icloud.com/database/1/com.apple.photos.cloud/...`

## Usage
```bash
# Login once (saves session)
photoscli export login

# Export to external drive (direct from iCloud, no local cache)
photoscli export --all /Volumes/Backup/photos
photoscli export ABC123 /Volumes/Backup/photos
```
