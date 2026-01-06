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
}

// NewClient creates a new iCloud client.
func NewClient() (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create cookie jar: %w", err)
	}

	return &Client{
		httpClient: &http.Client{Jar: jar},
		cookieJar:  jar,
	}, nil
}

// ImportCookies imports cookies from a browser export into the client.
func (c *Client) ImportCookies(cookies []*ImportedCookie) {
	// Base domains to set cookies on
	domains := []string{
		"https://www.icloud.com",
		"https://www.icloud.com.cn",
	}

	// Add PhotosURL domain if set
	if c.PhotosURL != "" {
		domains = append(domains, c.PhotosURL)
	}

	for _, domain := range domains {
		u, _ := url.Parse(domain)
		var httpCookies []*http.Cookie
		for _, ic := range cookies {
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
	rand.Read(b)
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
