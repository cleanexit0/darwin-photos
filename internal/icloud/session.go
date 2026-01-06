package icloud

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// Session holds persistent authentication state.
type Session struct {
	Dsid      string         `json:"dsid"`
	ClientID  string         `json:"client_id,omitempty"`
	PhotosURL string         `json:"photos_url"`
	Cookies   []*http.Cookie `json:"cookies"`
	SavedAt   time.Time      `json:"saved_at"`
}

// DefaultSessionPath returns the default session file path.
func DefaultSessionPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".darwin-photos", "session.json")
}

// SaveSession saves the client's session state to a file.
func (c *Client) SaveSession(path string) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create session directory: %w", err)
	}

	// Collect cookies from all relevant domains (including China)
	var cookies []*http.Cookie
	domains := []string{
		"https://www.icloud.com",
		"https://idmsa.apple.com",
		"https://setup.icloud.com",
		"https://www.icloud.com.cn",
		"https://setup.icloud.com.cn",
	}
	// Add the PhotosURL domain if it's set
	if c.PhotosURL != "" {
		domains = append(domains, c.PhotosURL)
	}
	for _, domain := range domains {
		u, _ := url.Parse(domain)
		cookies = append(cookies, c.cookieJar.Cookies(u)...)
	}

	session := Session{
		Dsid:      c.Dsid,
		ClientID:  c.ClientID,
		PhotosURL: c.PhotosURL,
		Cookies:   cookies,
		SavedAt:   time.Now(),
	}

	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write session file: %w", err)
	}

	return nil
}

// LoadSession loads a saved session from a file.
func (c *Client) LoadSession(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no saved session found")
		}
		return fmt.Errorf("failed to read session file: %w", err)
	}

	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return fmt.Errorf("failed to parse session file: %w", err)
	}

	// Restore state
	c.Dsid = session.Dsid
	c.ClientID = session.ClientID
	c.PhotosURL = session.PhotosURL

	// Restore cookies to all relevant domains (including China)
	// Note: PhotosURL may include partition-specific domain like p227-ckdatabasews.icloud.com.cn
	domains := []string{
		"https://www.icloud.com",
		"https://idmsa.apple.com",
		"https://setup.icloud.com",
		"https://www.icloud.com.cn",
		"https://setup.icloud.com.cn",
	}
	// Add the PhotosURL domain if it's set
	if session.PhotosURL != "" {
		domains = append(domains, session.PhotosURL)
	}
	for _, domain := range domains {
		u, _ := url.Parse(domain)
		c.cookieJar.SetCookies(u, session.Cookies)
	}

	return nil
}

// ClearSession removes the saved session file.
func ClearSession(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove session file: %w", err)
	}
	return nil
}

// IsLoggedIn checks if the client has valid session state.
func (c *Client) IsLoggedIn() bool {
	// Check if PhotosURL is set (cookies will be in the jar after LoadSession)
	return c.PhotosURL != ""
}
