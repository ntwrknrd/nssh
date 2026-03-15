package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/sync"
)

// ContainerlabProvider discovers nodes from containerlab via a jump host.
type ContainerlabProvider struct{}

// NewContainerlabProvider returns a new containerlab provider.
func NewContainerlabProvider() *ContainerlabProvider {
	return &ContainerlabProvider{}
}

// clabInspectOutput matches the top-level JSON from containerlab inspect --all --format json.
type clabInspectOutput struct {
	Containers []clabContainer `json:"containers"`
}

// clabContainer represents a single container from containerlab inspect output.
type clabContainer struct {
	Name        string `json:"name"`
	ContainerID string `json:"container_id"`
	Image       string `json:"image"`
	Kind        string `json:"kind"`
	State       string `json:"state"`
	IPv4Address string `json:"ipv4_address"`
	IPv6Address string `json:"ipv6_address"`
	LabName     string `json:"lab_name"`
	LabPath     string `json:"labPath"`
	Group       string `json:"group"`
	ShortName   string `json:"shortname"`
	Owner       string `json:"owner"`
}

// Discover connects to the configured jump host and runs containerlab inspect.
func (p *ContainerlabProvider) Discover(ctx context.Context, source config.SyncSourceConfig, runner sync.RemoteRunner) ([]sync.InventoryObject, error) {
	if source.Containerlab == nil {
		return nil, fmt.Errorf("containerlab config missing for source %q", source.Name)
	}

	clabCfg := source.Containerlab

	argv := []string{"containerlab", "inspect", "--all", "--format", "json"}

	cmd := sync.RemoteCommand{
		Argv:    argv,
		Sudo:    clabCfg.Sudo,
		Timeout: 30 * time.Second,
	}

	result, err := runner.Run(ctx, clabCfg.JumpHost, cmd)
	if err != nil {
		return nil, fmt.Errorf("remote exec on %s: %w", clabCfg.JumpHost, err)
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("containerlab inspect exited %d: %s", result.ExitCode, string(result.Stderr))
	}

	return ParseContainerlabJSON(result.Stdout, source.Name, clabCfg.JumpHost)
}

// ParseContainerlabJSON parses containerlab inspect JSON output and returns
// normalized inventory objects.
func ParseContainerlabJSON(data []byte, sourceName, jumpHost string) ([]sync.InventoryObject, error) {
	var output clabInspectOutput
	if err := json.Unmarshal(data, &output); err != nil {
		return nil, fmt.Errorf("parse containerlab JSON: %w", err)
	}

	var objects []sync.InventoryObject
	for i := range output.Containers {
		c := &output.Containers[i]
		hostname := stripCIDR(c.IPv4Address)
		if hostname == "" {
			hostname = stripCIDR(c.IPv6Address)
		}
		if hostname == "" {
			// No usable address, skip
			continue
		}

		objectID := fmt.Sprintf("%s/%s", c.LabName, c.ShortName)
		if c.LabName == "" {
			objectID = c.Name
		}

		obj := sync.InventoryObject{
			Provider:        "containerlab",
			Source:          sourceName,
			ObjectID:        objectID,
			ObjectType:      "node",
			Name:            c.Name,
			HostName:        hostname,
			ProxyJump:       jumpHost,
			UsesPassword:    true,
			CredentialClass: c.Kind,
			Attributes: map[string][]string{
				"kind":  {c.Kind},
				"lab":   {c.LabName},
				"state": {c.State},
				"image": {c.Image},
			},
		}

		objects = append(objects, obj)
	}

	return objects, nil
}

// stripCIDR removes a /prefix from an IP address string.
func stripCIDR(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" || addr == "N/A" {
		return ""
	}
	if idx := strings.Index(addr, "/"); idx != -1 {
		addr = addr[:idx]
	}
	if addr == "" {
		return ""
	}
	return addr
}
