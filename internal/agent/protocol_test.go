//go:build linux || darwin

package agent

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestProtocolVersion(t *testing.T) {
	if ProtocolVersion != 1 {
		t.Errorf("ProtocolVersion = %d, want 1", ProtocolVersion)
	}
}

func TestRequest_JSON(t *testing.T) {
	tests := []struct {
		name string
		req  Request
	}{
		{
			name: "hello",
			req:  Request{Version: 1, Op: OpHello},
		},
		{
			name: "status",
			req:  Request{Version: 1, Op: OpStatus, ID: "req-123"},
		},
		{
			name: "decrypt with data",
			req:  Request{Version: 1, Op: OpDecrypt, Data: []byte("ciphertext")},
		},
		{
			name: "recipient",
			req:  Request{Version: 1, Op: OpRecipient},
		},
		{
			name: "lock",
			req:  Request{Version: 1, Op: OpLock},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal to JSON
			data, err := json.Marshal(tt.req)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			// Unmarshal back
			var got Request
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			// Verify fields
			if got.Version != tt.req.Version {
				t.Errorf("Version = %d, want %d", got.Version, tt.req.Version)
			}
			if got.Op != tt.req.Op {
				t.Errorf("Op = %q, want %q", got.Op, tt.req.Op)
			}
			if got.ID != tt.req.ID {
				t.Errorf("ID = %q, want %q", got.ID, tt.req.ID)
			}
			if !bytes.Equal(got.Data, tt.req.Data) {
				t.Errorf("Data = %q, want %q", got.Data, tt.req.Data)
			}
		})
	}
}

func TestResponse_JSON(t *testing.T) {
	tests := []struct {
		name string
		resp Response
	}{
		{
			name: "success",
			resp: Response{OK: true, Data: []byte("result")},
		},
		{
			name: "error",
			resp: Response{OK: false, Err: "something went wrong"},
		},
		{
			name: "with id",
			resp: Response{ID: "req-123", OK: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal to JSON
			data, err := json.Marshal(tt.resp)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			// Unmarshal back
			var got Response
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			// Verify fields
			if got.OK != tt.resp.OK {
				t.Errorf("OK = %v, want %v", got.OK, tt.resp.OK)
			}
			if got.Err != tt.resp.Err {
				t.Errorf("Err = %q, want %q", got.Err, tt.resp.Err)
			}
			if got.ID != tt.resp.ID {
				t.Errorf("ID = %q, want %q", got.ID, tt.resp.ID)
			}
			if !bytes.Equal(got.Data, tt.resp.Data) {
				t.Errorf("Data = %q, want %q", got.Data, tt.resp.Data)
			}
		})
	}
}

func TestStatusInfo_JSON(t *testing.T) {
	info := StatusInfo{
		Mode:          ModeSoftware,
		IdleTimeout:   3600,
		MaxLifetime:   86400,
		RemainingLife: 43200,
		RemainingIdle: 1800,
	}

	// Marshal to JSON
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Unmarshal back
	var got StatusInfo
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Verify fields
	if got.Mode != info.Mode {
		t.Errorf("Mode = %q, want %q", got.Mode, info.Mode)
	}
	if got.IdleTimeout != info.IdleTimeout {
		t.Errorf("IdleTimeout = %d, want %d", got.IdleTimeout, info.IdleTimeout)
	}
	if got.MaxLifetime != info.MaxLifetime {
		t.Errorf("MaxLifetime = %d, want %d", got.MaxLifetime, info.MaxLifetime)
	}
	if got.RemainingLife != info.RemainingLife {
		t.Errorf("RemainingLife = %d, want %d", got.RemainingLife, info.RemainingLife)
	}
	if got.RemainingIdle != info.RemainingIdle {
		t.Errorf("RemainingIdle = %d, want %d", got.RemainingIdle, info.RemainingIdle)
	}
}

func TestOpConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		want     string
	}{
		{"OpHello", OpHello, "hello"},
		{"OpStatus", OpStatus, "status"},
		{"OpDecrypt", OpDecrypt, "decrypt"},
		{"OpRecipient", OpRecipient, "recipient"},
		{"OpLock", OpLock, "lock"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.constant, tt.want)
			}
		})
	}
}
