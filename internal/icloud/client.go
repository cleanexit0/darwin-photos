// Package icloud provides a client for downloading photos from iCloud via cookie-based auth.
package icloud

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

// ImportedCookie represents a cookie from a browser export.
type ImportedCookie struct {
	Domain string
	Path   string
	Secure bool
	Name   string
	Value  string
}

const (
	UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

// Client is an iCloud API client.
type Client struct {
	httpClient *http.Client
	cookieJar  *cookiejar.Jar

	// Auth state (extracted from cookies or set manually)
	Dsid     string // Directory Services ID (numeric for CloudKit dsid param)
	ClientID string // Unique client ID for CloudKit requests

	// Service URL (e.g., https://p227-ckdatabasews.icloud.com.cn)
	PhotosURL string

	// Stored imported cookies for setting on new domains dynamically
	importedCookies []*ImportedCookie
}

// NewClient creates a new iCloud client.
func NewClient() (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create cookie jar: %w", err)
	}

	return &Client{
		httpClient: &http.Client{
			Jar:     jar,
			Timeout: 30 * time.Second,
		},
		cookieJar: jar,
	}, nil
}

// ImportCookies imports cookies from a browser export into the client.
func (c *Client) ImportCookies(cookies []*ImportedCookie) {
	// Store cookies for dynamic domain addition later
	c.importedCookies = cookies

	// Base domains to set cookies on (including setup endpoints)
	domains := []string{
		"https://www.icloud.com",
		"https://www.icloud.com.cn",
		"https://setup.icloud.com",
		"https://setup.icloud.com.cn",
	}

	// Add PhotosURL domain if set
	if c.PhotosURL != "" {
		domains = append(domains, c.PhotosURL)
	}

	for _, domain := range domains {
		c.setCookiesForDomain(domain)
	}
}

// setCookiesForDomain sets matching imported cookies on a specific domain.
func (c *Client) setCookiesForDomain(domain string) {
	u, _ := url.Parse(domain)
	var httpCookies []*http.Cookie
	for _, ic := range c.importedCookies {
		// Match cookies to domain (handle wildcard domains like .icloud.com.cn)
		cookieDomain := strings.TrimPrefix(ic.Domain, ".")
		if strings.HasSuffix(u.Host, cookieDomain) || u.Host == cookieDomain {
			httpCookies = append(httpCookies, &http.Cookie{
				Name:   ic.Name,
				Value:  ic.Value,
				Path:   ic.Path,
				Secure: ic.Secure,
			})
		}
	}
	if len(httpCookies) > 0 {
		c.cookieJar.SetCookies(u, httpCookies)
	}
}

// getClientID returns a unique client ID for CloudKit requests.
func (c *Client) getClientID() string {
	if c.ClientID == "" {
		c.ClientID = generateUUID()
	}
	return c.ClientID
}

// generateUUID generates a random UUID v4.
func generateUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// doRequest performs an HTTP request with required iCloud headers.
func (c *Client) doRequest(method, reqURL string, body interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequest(method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set Origin/Referer based on domain
	if strings.Contains(reqURL, ".cn") {
		req.Header.Set("Origin", "https://www.icloud.com.cn")
		req.Header.Set("Referer", "https://www.icloud.com.cn/")
	} else {
		req.Header.Set("Origin", "https://www.icloud.com")
		req.Header.Set("Referer", "https://www.icloud.com/")
	}

	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "*/*")

	if body != nil {
		// CloudKit uses text/plain, others use application/json
		if strings.Contains(reqURL, "ckdatabasews") {
			req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
		} else {
			req.Header.Set("Content-Type", "application/json")
		}
	}

	return c.httpClient.Do(req)
}

// DiscoverPhotosURL calls iCloud setup endpoint to discover the ckdatabasews URL.
// Returns the discovered URL and any error.
func (c *Client) DiscoverPhotosURL() (string, error) {
	// Determine which setup endpoint to use based on cookies
	// Try China endpoint first if we have .cn cookies
	endpoints := []string{
		"https://setup.icloud.com.cn/setup/ws/1",
		"https://setup.icloud.com/setup/ws/1",
	}

	var lastErr error
	for _, endpoint := range endpoints {
		photosURL, err := c.tryDiscoverURL(endpoint, 0)
		if err != nil {
			lastErr = err
			continue
		}
		if photosURL != "" {
			return photosURL, nil
		}
	}

	if lastErr != nil {
		return "", fmt.Errorf("failed to discover photos URL: %w", lastErr)
	}
	return "", fmt.Errorf("failed to discover photos URL from any endpoint")
}

func (c *Client) tryDiscoverURL(setupEndpoint string, depth int) (string, error) {
	// Prevent infinite recursion
	if depth > 3 {
		return "", fmt.Errorf("too many redirects (depth %d) - session may be invalid, please export fresh cookies from your browser", depth)
	}

	// Build validate URL with required query parameters
	validateURL := setupEndpoint + "/validate"
	queryParams := url.Values{}
	queryParams.Set("clientBuildNumber", "2546Build34")
	queryParams.Set("clientMasteringNumber", "2546Build34")
	queryParams.Set("clientId", c.getClientID())
	queryParams.Set("requestId", generateUUID())
	validateURL += "?" + queryParams.Encode()
	resp, err := c.doRequest("POST", validateURL, nil)
	if err != nil {
		return "", fmt.Errorf("validate request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == 421 {
		// Need partition-specific server
		partition := resp.Header.Get("X-Apple-User-Partition")
		if partition != "" {
			var partitionEndpoint string
			var partitionDomain string
			if strings.Contains(setupEndpoint, ".cn") {
				partitionDomain = fmt.Sprintf("https://p%s-setup.icloud.com.cn", partition)
				partitionEndpoint = partitionDomain + "/setup/ws/1"
			} else {
				partitionDomain = fmt.Sprintf("https://p%s-setup.icloud.com", partition)
				partitionEndpoint = partitionDomain + "/setup/ws/1"
			}
			// Set cookies on the partition-specific domain before retrying
			c.setCookiesForDomain(partitionDomain)
			return c.tryDiscoverURL(partitionEndpoint, depth+1)
		}
	}

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("validate failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Webservices struct {
			Ckdatabasews struct {
				URL string `json:"url"`
			} `json:"ckdatabasews"`
		} `json:"webservices"`
		DsInfo struct {
			Dsid string `json:"dsid"`
		} `json:"dsInfo"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	// Update DSID if found
	if result.DsInfo.Dsid != "" {
		c.Dsid = result.DsInfo.Dsid
	}

	return result.Webservices.Ckdatabasews.URL, nil
}
