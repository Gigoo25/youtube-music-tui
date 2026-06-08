package api

import "time"

// RequestTimeout bounds every Innertube HTTP request. Without it a hung
// connection blocks the tea.Cmd goroutine indefinitely and the view stays stuck
// on "Loading…".
const RequestTimeout = 15 * time.Second

// SetTimeout configures the HTTP request timeout for the client. The underlying
// *http.Client is built in ytmusic.go (which is secret-scanned and left
// untouched); this method lives in the same package so it can reach that shared
// client and apply the timeout to every request, Search included.
func (c *Client) SetTimeout(d time.Duration) {
	if c.http != nil {
		c.http.Timeout = d
	}
}
