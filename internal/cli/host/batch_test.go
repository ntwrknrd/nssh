package host

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsBatchFile(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		want bool
	}{
		{"csv file", "hosts.csv", true},
		{"json file", "hosts.json", true},
		{"mixed case CSV", "Hosts.Csv", true},
		{"uppercase JSON", "HOSTS.JSON", true},
		{"not batch - txt", "hosts.txt", false},
		{"not batch - no extension", "hosts", false},
		{"not batch - yaml", "hosts.yaml", false},
		{"not batch - fqdn", "server.example.com", false},
		{"not batch - path with dot", "/path/to/file", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsBatchFile(tt.arg); got != tt.want {
				t.Errorf("IsBatchFile(%q) = %v, want %v", tt.arg, got, tt.want)
			}
		})
	}
}

func TestValidateHostname(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
		wantErr  bool
	}{
		{"valid fqdn", "server.example.com", false},
		{"valid short", "server", false},
		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"contains space", "server name", true},
		{"contains tab", "server\tname", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHostname(tt.hostname)
			if (err != "") != tt.wantErr {
				t.Errorf("ValidateHostname(%q) error = %v, wantErr %v", tt.hostname, err, tt.wantErr)
			}
		})
	}
}

func TestParseTxtFile_Unsupported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts.txt")

	if err := os.WriteFile(path, []byte("server1.example.com\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ParseBatchFile(path)
	if err == nil {
		t.Error("ParseBatchFile() expected error for .txt file")
	}
}

func TestParseCsvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts.csv")

	content := `host,hostname,user,port,context
server1.example.com,192.168.1.10,admin,22,prod
server2.example.com,,root,2222,dev
server3.example.com,,,,staging
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	entries, err := ParseBatchFile(path)
	if err != nil {
		t.Fatalf("ParseBatchFile() error = %v", err)
	}

	if len(entries) != 3 {
		t.Errorf("got %d entries, want 3", len(entries))
	}

	// Check first entry - has HostName override
	e := entries[0]
	if e.Host != "server1.example.com" || e.HostName != "192.168.1.10" || e.User != "admin" || e.Port != 22 || e.Context != "prod" {
		t.Errorf("entry[0] = %+v, unexpected values", e)
	}

	// Check second entry - port should be 2222, no HostName override
	if entries[1].Port != 2222 || entries[1].HostName != "" {
		t.Errorf("entry[1].Port = %d, HostName = %q, want 2222 and empty", entries[1].Port, entries[1].HostName)
	}

	// Check third entry - host only
	if entries[2].Host != "server3.example.com" {
		t.Errorf("entry[2].Host = %q, want 'server3.example.com'", entries[2].Host)
	}
}

func TestParseJsonFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts.json")

	content := `[
  {"host": "server1.example.com", "user": "admin", "port": 22, "context": "prod"},
  {"host": "server2.example.com", "hostname": "10.0.0.2"},
  {"host": "server3.example.com"}
]`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	entries, err := ParseBatchFile(path)
	if err != nil {
		t.Fatalf("ParseBatchFile() error = %v", err)
	}

	if len(entries) != 3 {
		t.Errorf("got %d entries, want 3", len(entries))
	}

	// Check first entry
	if entries[0].User != "admin" || entries[0].Port != 22 {
		t.Errorf("entry[0] = %+v, unexpected values", entries[0])
	}

	// Check second entry - has hostName override
	if entries[1].HostName != "10.0.0.2" {
		t.Errorf("entry[1].HostName = %q, want '10.0.0.2'", entries[1].HostName)
	}
}

func TestParseBatchFile_NotFound(t *testing.T) {
	_, err := ParseBatchFile("/nonexistent/path.csv")
	if err == nil {
		t.Error("ParseBatchFile() expected error for nonexistent file")
	}
}

func TestParseBatchFile_TooLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.csv")

	// Create a file larger than MaxBatchFileSize (10MB)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	// Write 11MB of data
	data := make([]byte, 11*1024*1024)
	_, _ = f.Write(data)
	_ = f.Close()

	_, err = ParseBatchFile(path)
	if err == nil {
		t.Error("ParseBatchFile() expected error for file > 10MB")
	}
}

func TestBatchResult(t *testing.T) {
	r := &BatchResult{
		Added:   5,
		Skipped: 2,
		Failed:  1,
		Errors:  []string{"error1"},
	}

	if r.TotalProcessed() != 8 {
		t.Errorf("TotalProcessed() = %d, want 8", r.TotalProcessed())
	}

	if !r.HasFailures() {
		t.Error("HasFailures() = false, want true")
	}

	r2 := &BatchResult{Added: 5}
	if r2.HasFailures() {
		t.Error("HasFailures() = true, want false")
	}
}
