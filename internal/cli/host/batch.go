package host

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// MaxBatchFileSize is the maximum size of a batch file (10MB).
// This prevents memory exhaustion from maliciously large files.
const MaxBatchFileSize = 10 * 1024 * 1024

// BatchResult tracks the outcome of a batch operation.
type BatchResult struct {
	Added   int
	Skipped int
	Failed  int
	Errors  []string
}

// TotalProcessed returns the total number of entries processed.
func (r *BatchResult) TotalProcessed() int {
	return r.Added + r.Skipped + r.Failed
}

// HasFailures returns true if any entries failed.
func (r *BatchResult) HasFailures() bool {
	return r.Failed > 0
}

// HostEntry represents a host from a batch file.
type HostEntry struct {
	Host     string `json:"host"`               // The SSH Host identifier (unique within a context)
	HostName string `json:"hostname,omitempty"` // The SSH HostName (resolved address, FQDN or IP)
	User     string `json:"user,omitempty"`
	Port     int    `json:"port,omitempty"`
	Context  string `json:"context,omitempty"`
	Password string `json:"password,omitempty"`
}

// IsBatchFile detects if an argument is a batch file based on extension.
// Supported formats: CSV (.csv) and JSON (.json).
func IsBatchFile(arg string) bool {
	ext := strings.ToLower(filepath.Ext(arg))
	switch ext {
	case ".csv", ".json":
		return true
	default:
		return false
	}
}

// ValidateHostname validates a hostname format.
// Returns an error message if invalid, empty string if valid.
func ValidateHostname(hostname string) string {
	if hostname == "" {
		return "hostname is required"
	}
	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		return "hostname cannot be empty"
	}
	if strings.ContainsAny(hostname, " \t\n\r") {
		return fmt.Sprintf("invalid hostname format: %s", hostname)
	}
	return ""
}

// ParseBatchFile parses a batch file into HostEntry objects based on extension.
// Supported formats: CSV (.csv) and JSON (.json).
func ParseBatchFile(path string) ([]HostEntry, error) {
	// Check file exists
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("batch file not found: %s", path)
		}
		return nil, fmt.Errorf("stat batch file: %w", err)
	}

	// Check file size
	if fi.Size() > MaxBatchFileSize {
		return nil, fmt.Errorf("batch file too large (%.1f MB). Maximum allowed: %.0f MB",
			float64(fi.Size())/(1024*1024),
			float64(MaxBatchFileSize)/(1024*1024))
	}

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".csv":
		return parseCsvFile(path)
	case ".json":
		return parseJsonFile(path)
	default:
		return nil, fmt.Errorf("unsupported file format: %s (use .csv or .json)", ext)
	}
}

// parseCsvFile parses a .csv file with headers into HostEntry objects.
// Expected headers: host, hostname, user, port, context, password
// - host: The SSH Host identifier (required, what users type to connect)
// - hostname: The SSH HostName (optional, resolved address - use for IP when DNS unavailable)
func parseCsvFile(path string) ([]HostEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	reader := csv.NewReader(f)

	// Read header
	header, err := reader.Read()
	if err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("CSV file is empty")
		}
		return nil, fmt.Errorf("read CSV header: %w", err)
	}

	// Build column index map
	colIdx := make(map[string]int)
	for i, col := range header {
		colIdx[strings.ToLower(strings.TrimSpace(col))] = i
	}

	// Host is required
	if _, ok := colIdx["host"]; !ok {
		return nil, fmt.Errorf("CSV file missing required 'host' column")
	}

	var entries []HostEntry
	lineNum := 1 // header was line 1

	for {
		lineNum++
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNum, err)
		}

		entry := HostEntry{}

		// Get host (required) - the SSH Host identifier
		if idx, ok := colIdx["host"]; ok && idx < len(record) {
			entry.Host = strings.TrimSpace(record[idx])
		}
		if entry.Host == "" {
			continue // Skip empty rows
		}

		// Get optional hostname - the SSH HostName (resolved address)
		if idx, ok := colIdx["hostname"]; ok && idx < len(record) {
			entry.HostName = strings.TrimSpace(record[idx])
		}
		if idx, ok := colIdx["user"]; ok && idx < len(record) {
			entry.User = strings.TrimSpace(record[idx])
		}
		if idx, ok := colIdx["port"]; ok && idx < len(record) {
			if portStr := strings.TrimSpace(record[idx]); portStr != "" {
				port, err := strconv.Atoi(portStr)
				if err != nil {
					return nil, fmt.Errorf("line %d: invalid port '%s'", lineNum, portStr)
				}
				entry.Port = port
			}
		}
		if idx, ok := colIdx["context"]; ok && idx < len(record) {
			entry.Context = strings.TrimSpace(record[idx])
		}
		if idx, ok := colIdx["password"]; ok && idx < len(record) {
			entry.Password = strings.TrimSpace(record[idx])
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

// parseJsonFile parses a .json file (array of objects) into HostEntry objects.
func parseJsonFile(path string) ([]HostEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var entries []HostEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	// Filter out entries with empty host
	var filtered []HostEntry
	for _, e := range entries {
		if e.Host != "" {
			filtered = append(filtered, e)
		}
	}

	return filtered, nil
}
