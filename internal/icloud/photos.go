package icloud

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
)

// PhotoAsset represents a photo from iCloud Photos.
type PhotoAsset struct {
	RecordName  string
	Filename    string
	Size        int64
	DownloadURL string
	Checksum    string
}

// CloudKitZoneID identifies a CloudKit zone.
type CloudKitZoneID struct {
	ZoneName string `json:"zoneName"`
}

// CloudKitLookup is a request to look up specific records.
type CloudKitLookup struct {
	Records []CloudKitRecordRef `json:"records"`
	ZoneID  CloudKitZoneID      `json:"zoneID"`
}

// CloudKitRecordRef is a reference to a record.
type CloudKitRecordRef struct {
	RecordName string `json:"recordName"`
}

// GetDownloadURL queries CloudKit to get the download URL for a photo by its CloudKit GUID.
func (c *Client) GetDownloadURL(cloudKitGUID string) (*PhotoAsset, error) {
	if c.PhotosURL == "" {
		return nil, fmt.Errorf("photos URL not set - run 'import-cookies' first")
	}

	// Build CloudKit lookup URL
	ckURL := c.PhotosURL + "/database/1/com.apple.photos.cloud/production/private/records/lookup"
	queryParams := url.Values{}
	queryParams.Set("remapEnums", "true")
	if c.Dsid != "" {
		queryParams.Set("dsid", c.Dsid)
	}
	queryParams.Set("clientBuildNumber", "2546Build17")
	queryParams.Set("clientMasteringNumber", "2546Build17")
	queryParams.Set("clientId", c.getClientID())
	ckURL += "?" + queryParams.Encode()

	lookup := CloudKitLookup{
		Records: []CloudKitRecordRef{{RecordName: cloudKitGUID}},
		ZoneID:  CloudKitZoneID{ZoneName: "PrimarySync"},
	}

	resp, err := c.doRequest("POST", ckURL, lookup)
	if err != nil {
		return nil, fmt.Errorf("cloudkit lookup failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("cloudkit lookup failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Records []struct {
			RecordName      string `json:"recordName"`
			ServerErrorCode string `json:"serverErrorCode,omitempty"`
			Fields          struct {
				ResOriginalRes struct {
					Value struct {
						DownloadURL string `json:"downloadURL"`
						Size        int64  `json:"size"`
						Checksum    string `json:"checksum"`
					} `json:"value"`
				} `json:"resOriginalRes"`
				FilenameEnc struct {
					Value string `json:"value"`
				} `json:"filenameEnc"`
			} `json:"fields"`
		} `json:"records"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to decode cloudkit response: %w", err)
	}

	if len(result.Records) == 0 {
		return nil, fmt.Errorf("photo not found: %s", cloudKitGUID)
	}

	record := result.Records[0]

	// Check for server error
	if record.ServerErrorCode != "" {
		return nil, fmt.Errorf("cloudkit error for %s: %s", cloudKitGUID, record.ServerErrorCode)
	}

	// Decode base64 encoded filename
	filename := ""
	if record.Fields.FilenameEnc.Value != "" {
		decoded, err := base64.StdEncoding.DecodeString(record.Fields.FilenameEnc.Value)
		if err == nil {
			filename = string(decoded)
		}
	}

	asset := &PhotoAsset{
		RecordName:  record.RecordName,
		Filename:    filename,
		Size:        record.Fields.ResOriginalRes.Value.Size,
		DownloadURL: record.Fields.ResOriginalRes.Value.DownloadURL,
		Checksum:    record.Fields.ResOriginalRes.Value.Checksum,
	}

	if asset.DownloadURL == "" {
		return nil, fmt.Errorf("no download URL available for: %s", cloudKitGUID)
	}

	return asset, nil
}

// GetDownloadURLs queries CloudKit for multiple photos by CloudKit GUID.
func (c *Client) GetDownloadURLs(cloudKitGUIDs []string) (map[string]*PhotoAsset, error) {
	if c.PhotosURL == "" {
		return nil, fmt.Errorf("photos URL not set - run 'import-cookies' first")
	}

	refs := make([]CloudKitRecordRef, len(cloudKitGUIDs))
	for i, guid := range cloudKitGUIDs {
		refs[i] = CloudKitRecordRef{RecordName: guid}
	}

	ckURL := c.PhotosURL + "/database/1/com.apple.photos.cloud/production/private/records/lookup"
	queryParams := url.Values{}
	queryParams.Set("remapEnums", "true")
	if c.Dsid != "" {
		queryParams.Set("dsid", c.Dsid)
	}
	queryParams.Set("clientBuildNumber", "2546Build17")
	queryParams.Set("clientMasteringNumber", "2546Build17")
	queryParams.Set("clientId", c.getClientID())
	ckURL += "?" + queryParams.Encode()

	lookup := CloudKitLookup{
		Records: refs,
		ZoneID:  CloudKitZoneID{ZoneName: "PrimarySync"},
	}

	resp, err := c.doRequest("POST", ckURL, lookup)
	if err != nil {
		return nil, fmt.Errorf("cloudkit lookup failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("cloudkit lookup failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Records []struct {
			RecordName string `json:"recordName"`
			Fields     struct {
				ResOriginalRes struct {
					Value struct {
						DownloadURL string `json:"downloadURL"`
						Size        int64  `json:"size"`
						Checksum    string `json:"checksum"`
					} `json:"value"`
				} `json:"resOriginalRes"`
				FilenameEnc struct {
					Value string `json:"value"`
				} `json:"filenameEnc"`
			} `json:"fields"`
		} `json:"records"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode cloudkit response: %w", err)
	}

	assets := make(map[string]*PhotoAsset)
	for _, record := range result.Records {
		filename := ""
		if record.Fields.FilenameEnc.Value != "" {
			decoded, err := base64.StdEncoding.DecodeString(record.Fields.FilenameEnc.Value)
			if err == nil {
				filename = string(decoded)
			}
		}

		if record.Fields.ResOriginalRes.Value.DownloadURL != "" {
			assets[record.RecordName] = &PhotoAsset{
				RecordName:  record.RecordName,
				Filename:    filename,
				Size:        record.Fields.ResOriginalRes.Value.Size,
				DownloadURL: record.Fields.ResOriginalRes.Value.DownloadURL,
				Checksum:    record.Fields.ResOriginalRes.Value.Checksum,
			}
		}
	}

	return assets, nil
}
