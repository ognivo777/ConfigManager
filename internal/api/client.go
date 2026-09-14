package api

import (
	"encoding/json"
)

// Client is a minimal Unix socket client for the cm API.
type Client struct {
	socket string
}

// NewClient returns a client for the given socket path.
func NewClient(socket string) *Client {
	return &Client{socket: socket}
}

// Call sends a request and decodes the response.
func (c *Client) Call(method string, params any, out any) error {
	conn, err := DialUnix(c.socket)
	if err != nil {
		return err
	}
	defer conn.Close()

	req := Request{Version: Version, Method: method}
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return err
		}
		req.Params = b
	}

	if err := writeJSON(conn, &req); err != nil {
		return err
	}
	var resp Response
	if err := readJSON(conn, &resp); err != nil {
		return err
	}
	if !resp.OK {
		return &RemoteError{Message: resp.Error}
	}
	if out != nil && resp.Result != nil {
		return json.Unmarshal(resp.Result, out)
	}
	return nil
}

// RemoteError is an error returned by the daemon.
type RemoteError struct{ Message string }

func (e *RemoteError) Error() string { return e.Message }
