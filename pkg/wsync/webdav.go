// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wsync

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const (
	webdavTimeout    = 30 * time.Second
	StateFilePrefix  = "state."
	StateFileSuffix  = ".json"
	webdavPropfindXML = `<?xml version="1.0"?><d:propfind xmlns:d="DAV:"><d:prop><d:getlastmodified/></d:prop></d:propfind>`
)

// WebDAVConfig is the connection info for the shared sync folder. Password is a
// Nextcloud app-password — kept in the secret store, never in plaintext settings.
type WebDAVConfig struct {
	BaseURL  string // e.g. https://host/remote.php/dav/files/<user>
	Folder   string // e.g. waveterm-sync
	User     string
	Password string
}

// WebDAVClient is a minimal WebDAV client — only the verbs sync needs.
type WebDAVClient struct {
	cfg  WebDAVConfig
	http *http.Client
}

func MakeWebDAVClient(cfg WebDAVConfig) *WebDAVClient {
	return &WebDAVClient{cfg: cfg, http: &http.Client{Timeout: webdavTimeout}}
}

// fileURL builds the absolute URL for a file inside the sync folder, escaping
// each path segment so installids / names with odd chars stay valid.
func (c *WebDAVClient) fileURL(name string) string {
	return joinURL(c.cfg.BaseURL, c.cfg.Folder, name)
}

func (c *WebDAVClient) folderURL() string {
	return joinURL(c.cfg.BaseURL, c.cfg.Folder)
}

// joinURL joins a base URL with additional path segments, escaping each segment
// and collapsing slashes. The base may already contain a path.
func joinURL(base string, segments ...string) string {
	out := strings.TrimRight(base, "/")
	for _, seg := range segments {
		seg = strings.Trim(seg, "/")
		if seg == "" {
			continue
		}
		out += "/" + url.PathEscape(seg)
	}
	return out
}

func (c *WebDAVClient) do(ctx context.Context, method, rawURL string, body []byte, hdr map[string]string) (*http.Response, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, rdr)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.cfg.User, c.cfg.Password)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	return c.http.Do(req)
}

// EnsureFolder creates the sync folder if it does not already exist (MKCOL is a
// no-op / 405 when it exists, which we treat as success).
func (c *WebDAVClient) EnsureFolder(ctx context.Context) error {
	resp, err := c.do(ctx, "MKCOL", c.folderURL(), nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// 201 created, 405 already exists, 301/302 some servers — all fine
	if resp.StatusCode == 201 || resp.StatusCode == 405 || resp.StatusCode == 301 || resp.StatusCode == 302 {
		return nil
	}
	return fmt.Errorf("webdav MKCOL %s: %s", c.folderURL(), resp.Status)
}

// Get reads a file; returns (nil, false, nil) when it does not exist (404).
func (c *WebDAVClient) Get(ctx context.Context, name string) ([]byte, bool, error) {
	resp, err := c.do(ctx, http.MethodGet, c.fileURL(name), nil, nil)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, false, nil
	}
	if resp.StatusCode != 200 {
		return nil, false, fmt.Errorf("webdav GET %s: %s", name, resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

// Put writes a file (overwriting). Single-writer-per-file means this never races
// another machine's write of the same name.
func (c *WebDAVClient) Put(ctx context.Context, name string, data []byte) error {
	resp, err := c.do(ctx, http.MethodPut, c.fileURL(name), data, map[string]string{"Content-Type": "application/json"})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 || resp.StatusCode == 201 || resp.StatusCode == 204 {
		return nil
	}
	return fmt.Errorf("webdav PUT %s: %s", name, resp.Status)
}

// Delete removes a file; a 404 is treated as success (already gone).
func (c *WebDAVClient) Delete(ctx context.Context, name string) error {
	resp, err := c.do(ctx, http.MethodDelete, c.fileURL(name), nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 || resp.StatusCode == 204 || resp.StatusCode == 404 {
		return nil
	}
	return fmt.Errorf("webdav DELETE %s: %s", name, resp.Status)
}

// ListStateFiles returns the names of all state.<installid>.json files in the
// sync folder (depth-1 PROPFIND), so a machine can pull every peer's state.
func (c *WebDAVClient) ListStateFiles(ctx context.Context) ([]string, error) {
	resp, err := c.do(ctx, "PROPFIND", c.folderURL(), []byte(webdavPropfindXML), map[string]string{
		"Depth":        "1",
		"Content-Type": "application/xml",
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 207 {
		return nil, fmt.Errorf("webdav PROPFIND %s: %s", c.folderURL(), resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return parseStateFileNames(data)
}

type propfindMultistatus struct {
	XMLName   xml.Name `xml:"multistatus"`
	Responses []struct {
		Href string `xml:"href"`
	} `xml:"response"`
}

// parseStateFileNames extracts state.*.json basenames from a PROPFIND multistatus
// body, ignoring the folder entry itself and any other files.
func parseStateFileNames(body []byte) ([]string, error) {
	var ms propfindMultistatus
	if err := xml.Unmarshal(body, &ms); err != nil {
		return nil, fmt.Errorf("parsing PROPFIND response: %w", err)
	}
	var names []string
	for _, r := range ms.Responses {
		href := strings.TrimRight(r.Href, "/")
		base := path.Base(href)
		if unescaped, err := url.PathUnescape(base); err == nil {
			base = unescaped
		}
		if strings.HasPrefix(base, StateFilePrefix) && strings.HasSuffix(base, StateFileSuffix) {
			names = append(names, base)
		}
	}
	return names, nil
}

// StateFileName is the file a given install writes (and the only one it writes).
func StateFileName(installId string) string {
	return StateFilePrefix + installId + StateFileSuffix
}
