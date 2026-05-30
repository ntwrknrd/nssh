//go:build linux || darwin

package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"
)

// ErrAgentNotRunning indicates no agent is listening on the socket.
var ErrAgentNotRunning = errors.New("agent not running")

// Client connects to a running agent and sends requests.
type Client struct {
	conn    net.Conn
	encoder *json.Encoder
	decoder *json.Decoder
}

// Connect establishes a connection to the running agent.
// Returns ErrAgentNotRunning if no agent is listening.
func Connect() (*Client, error) {
	conn, err := net.Dial("unix", SocketPath())
	if err != nil {
		return nil, ErrAgentNotRunning
	}

	return &Client{
		conn:    conn,
		encoder: json.NewEncoder(conn),
		decoder: json.NewDecoder(conn),
	}, nil
}

// Hello sends a hello request to verify the agent is responsive.
// Returns the agent's security mode (e.g., "software").
func (c *Client) Hello() (string, error) {
	resp, err := c.request(Request{Version: ProtocolVersion, Op: OpHello})
	if err != nil {
		return "", err
	}
	return string(resp.Data), nil
}

func (c *Client) MetadataGet(key string) (bool, []byte, error) {
	resp, err := c.request(Request{Version: ProtocolVersion, Op: OpMetadataGet, Key: key})
	if err != nil {
		return false, nil, err
	}
	if !resp.Found {
		return false, nil, nil
	}
	return true, resp.Data, nil
}

func (c *Client) MetadataPut(key string, data []byte) error {
	_, err := c.request(Request{Version: ProtocolVersion, Op: OpMetadataPut, Key: key, Data: data})
	return err
}

func (c *Client) MetadataDelete(key string) error {
	_, err := c.request(Request{Version: ProtocolVersion, Op: OpMetadataDelete, Key: key})
	return err
}

func (c *Client) MetadataDeletePrefix(prefix string) error {
	_, err := c.request(Request{Version: ProtocolVersion, Op: OpMetadataDeletePrefix, Key: prefix})
	return err
}

func (c *Client) MetadataClear() error {
	_, err := c.request(Request{Version: ProtocolVersion, Op: OpMetadataClear})
	return err
}

func (c *Client) ProviderRequest(req ProviderRequest) (*ProviderResponse, error) {
	resp, err := c.request(Request{Version: ProtocolVersion, Op: OpProviderRequest, Provider: &req})
	if err != nil {
		return nil, err
	}
	if resp.Provider == nil {
		return &ProviderResponse{}, nil
	}
	return resp.Provider, nil
}

// Status returns session status including timing information.
func (c *Client) Status() (*StatusInfo, error) {
	resp, err := c.request(Request{Version: ProtocolVersion, Op: OpStatus})
	if err != nil {
		return nil, err
	}
	var info StatusInfo
	if err := json.Unmarshal(resp.Data, &info); err != nil {
		return nil, fmt.Errorf("decode status: %w", err)
	}
	return &info, nil
}

// Lock sends a lock command to terminate the agent.
func (c *Client) Lock() error {
	_, err := c.request(Request{Version: ProtocolVersion, Op: OpLock})
	return err
}

// Close closes the connection to the agent.
func (c *Client) Close() error {
	return c.conn.Close()
}

// request sends a request and returns the response.
func (c *Client) request(req Request) (*Response, error) {
	// Set deadline per request so long-lived connections don't timeout
	if err := c.conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return nil, fmt.Errorf("agent %s: set deadline: %w", req.Op, err)
	}

	if err := c.encoder.Encode(req); err != nil {
		return nil, fmt.Errorf("agent %s: encode request: %w", req.Op, err)
	}

	var resp Response
	if err := c.decoder.Decode(&resp); err != nil {
		return nil, fmt.Errorf("agent %s: decode response: %w", req.Op, err)
	}

	if !resp.OK {
		return nil, fmt.Errorf("agent %s: %s", req.Op, resp.Err)
	}
	return &resp, nil
}
