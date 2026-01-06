# Implement iCloud Web API for Direct Downloads

## Goal

Add direct iCloud download capability to photoscli, bypassing the local Photos library cache. This enables backing up photos directly to external storage without filling local disk.

## Architecture

```
internal/icloud/
  client.go       # HTTP client with headers, cookie jar
  auth.go         # Login flow (username/password)
  twofactor.go    # 2FA device selection and code validation
  session.go      # Cookie persistence, session refresh
  photos.go       # Query photos/albums from CloudKit
  download.go     # Fetch files from download URLs

cmd/
  icloud.go       # New command: photoscli icloud ...
```

## Implementation Phases

### Phase 1: HTTP Client & Session Management

**File: `internal/icloud/client.go`**

```go
type Client struct {
    httpClient  *http.Client
    cookieJar   *cookiejar.Jar
    sessionFile string
    clientID    string

    // Auth state
    dsid        string  // Directory Services ID
    sessionToken string
    scnt        string  // Session count token
}

const (
    SetupEndpoint = "https://setup.icloud.com/setup/ws/1"
    AuthEndpoint  = "https://idmsa.apple.com/appleauth/auth"
)
```

Required headers for all requests:
- `Origin: https://www.icloud.com`
- `Referer: https://www.icloud.com/`
- `User-Agent: Mozilla/5.0 ...`
- `X-Apple-Widget-Key: <widget_key>`

**File: `internal/icloud/session.go`**

- Save/load cookies to `~/.photoscli/session.json`
- Save auth tokens for session reuse
- Detect expired sessions and prompt re-auth

### Phase 2: Authentication

**File: `internal/icloud/auth.go`**

```go
func (c *Client) Login(username, password string) error {
    // 1. POST to /signin/init - get salt, iterations, protocol
    // 2. Derive key using PBKDF2-SHA256
    // 3. POST to /signin/complete - submit password proof
    // 4. Handle response: success, 2FA required, or error
}
```

**Endpoints:**
1. `POST https://idmsa.apple.com/appleauth/auth/signin/init`
   - Body: `{"accountName": "user@email.com", "rememberMe": true}`
   - Returns: `salt`, `iterations`, `protocol`, `b` (server public key)

2. `POST https://idmsa.apple.com/appleauth/auth/signin/complete`
   - Body: SRP proof (`A`, `M1`)
   - Returns: session tokens or 2FA challenge

**File: `internal/icloud/twofactor.go`**

```go
func (c *Client) Handle2FA() error {
    // 1. GET /auth/2fa/trusteddevices - list devices
    // 2. Prompt user to select device or use SMS
    // 3. POST /auth/2fa/verify/trusteddevice/securitycode
    // 4. Validate code and get session
}
```

2FA flow:
1. Detect `authType: "hsa2"` in login response
2. Apple sends push to trusted devices automatically
3. User enters 6-digit code
4. `POST /verify/trusteddevice/securitycode` with code
5. `POST /trust` to trust this device (extends session)

### Phase 3: Photos CloudKit API

**File: `internal/icloud/photos.go`**

```go
type PhotosService struct {
    client      *Client
    serviceRoot string  // e.g., "https://p123-ckdatabasews.icloud.com"
}

type PhotoAsset struct {
    RecordName   string
    Filename     string
    Size         int64
    Created      time.Time
    DownloadURL  string
    // ... other fields
}

func (s *PhotosService) ListPhotos(limit, offset int) ([]PhotoAsset, error) {
    // POST to /database/1/com.apple.photos.cloud/production/private/records/query
}
```

**Query endpoint:**
```
POST https://p{XX}-ckdatabasews.icloud.com/database/1/com.apple.photos.cloud/production/private/records/query
```

**Request body:**
```json
{
  "query": {
    "recordType": "CPLAsset",
    "filterBy": [{"fieldName": "startRank", "comparator": "EQUALS", "fieldValue": {"value": 0}}]
  },
  "zoneID": {"zoneName": "PrimarySync"},
  "desiredKeys": ["resOriginalRes", "filenameEnc", "resOriginalWidth", ...],
  "resultsLimit": 100
}
```

**Response contains:**
- `records[].fields.resOriginalRes.value.downloadURL` - direct download link
- `records[].fields.filenameEnc.value` - base64 encoded filename

### Phase 4: Download Implementation

**File: `internal/icloud/download.go`**

```go
func (s *PhotosService) DownloadPhoto(asset *PhotoAsset, outputPath string) error {
    // Direct GET to asset.DownloadURL
    // Stream to file (don't load into memory)
    // Verify size matches
}
```

Download URLs are pre-signed and time-limited. Format:
```
https://cvws.icloud-content.com/B/...?o=...&v=1&x=3&a=...&e=...&k=...&s=...
```

### Phase 5: CLI Commands

**File: `cmd/icloud.go`**

```go
// photoscli icloud login     - Authenticate with iCloud (saves session)
// photoscli icloud logout    - Clear saved session
// photoscli icloud download <uuid> <output-dir>  - Download single photo
// photoscli icloud download --batch <output-dir> [--limit N] [--workers N]  - Batch download
```

**Note:** No `icloud ls` needed - use existing `photoscli ls --cloud` which reads from Photos.sqlite. The UUIDs from local database work with iCloud web API.

## Files to Create

| File | Purpose | ~LOC |
|------|---------|------|
| `internal/icloud/client.go` | HTTP client, headers | 150 |
| `internal/icloud/session.go` | Cookie/token persistence | 100 |
| `internal/icloud/auth.go` | Login with SRP | 200 |
| `internal/icloud/twofactor.go` | 2FA handling | 150 |
| `internal/icloud/photos.go` | Query CloudKit for download URLs (UUID → downloadURL) | 250 |
| `internal/icloud/download.go` | Stream files from download URLs | 100 |
| `cmd/icloud.go` | CLI: login, logout, download | 200 |
| **Total** | | **~1150** |

**Note:** photos.go is still needed to translate UUIDs (from local DB) into download URLs (from iCloud API). The local database has metadata but not download URLs.

## Dependencies to Add

```go
// go.mod additions
require (
    github.com/1Password/srp       // SRP-6a implementation
    golang.org/x/term              // Secure password input
)
```

## Implementation Order

1. **client.go + session.go** - Foundation
2. **auth.go** - Basic login (no 2FA yet)
3. **twofactor.go** - Complete auth flow
4. **photos.go** - List photos
5. **download.go** - Download files
6. **cmd/icloud.go** - Wire up CLI

## Testing Plan

1. `photoscli icloud login` - Verify auth flow, 2FA prompt
2. `photoscli ls --cloud --limit 10` - List cloud photos (existing command)
3. `photoscli icloud download <uuid> /tmp` - Single photo download via web API
4. `photoscli icloud download --batch --limit 10 /tmp` - Batch download via web API

## Risks

| Risk | Mitigation |
|------|------------|
| Apple changes API | Monitor icloudpd/pyicloud for updates |
| Rate limiting | Add delays, respect 429 responses |
| Session expiry | Clear instructions to re-login |
| SRP implementation | Use battle-tested 1Password/srp library |

## Usage Example (End Goal)

```bash
# One-time login (saves session)
photoscli icloud login
Email: user@icloud.com
Password: ********
2FA Code: 123456
✓ Logged in successfully

# List cloud photos (existing command, reads local database)
photoscli ls --cloud --limit 5

# Download directly to external HDD (no local cache!)
photoscli icloud download --batch --workers 4 /Volumes/MyHDD/backup

# Or download specific photo
photoscli icloud download ABC123 /Volumes/MyHDD/backup
```
