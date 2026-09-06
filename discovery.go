package sensaa

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grandcat/zeroconf"
)

const (
	serviceType             = "_sensaa._tcp"
	serviceDomain           = "local."
	defaultDiscoveryTimeout = 2 * time.Second
)

// Discover finds Sensaa nodes advertised on the local network. Discovery
// collects responses for up to two seconds, or until ctx ends sooner.
func Discover(ctx context.Context) ([]Node, error) {
	resolver, err := zeroconf.NewResolver(zeroconf.SelectIPTraffic(zeroconf.IPv4))
	if err != nil {
		return nil, fmt.Errorf("start Sensaa discovery: %w", err)
	}
	discoveryCtx, cancel := context.WithTimeout(ctx, defaultDiscoveryTimeout)
	defer cancel()

	entries := make(chan *zeroconf.ServiceEntry)
	if err := resolver.Browse(discoveryCtx, serviceType, serviceDomain, entries); err != nil {
		return nil, fmt.Errorf("browse for Sensaa nodes: %w", err)
	}

	byID := make(map[string]Node)
	for entry := range entries {
		node, err := nodeFromEntry(entry)
		if err == nil {
			byID[node.id] = node
		}
	}
	if err := ctx.Err(); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return nil, err
	}

	nodes := make([]Node, 0, len(byID))
	for _, node := range byID {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].id < nodes[j].id })
	return nodes, nil
}

func nodeFromEntry(entry *zeroconf.ServiceEntry) (Node, error) {
	values := make(map[string]string, len(entry.Text))
	for _, item := range entry.Text {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			values[key] = value
		}
	}
	if values["ver"] != "1" {
		return Node{}, fmt.Errorf("unsupported protocol version %q", values["ver"])
	}
	if values["id"] == "" {
		return Node{}, errors.New("missing node ID")
	}
	name := values["name"]
	if name == "" {
		name = entry.Instance
	}
	if name == "" {
		name = values["id"]
	}
	capabilities := parseCapabilities(values["caps"])
	if len(capabilities) == 0 {
		return Node{}, errors.New("missing capabilities")
	}
	if entry.Port < 1 || entry.Port > 65535 {
		return Node{}, fmt.Errorf("invalid port %d", entry.Port)
	}
	var ip net.IP
	for _, candidate := range entry.AddrIPv4 {
		if candidate.To4() != nil {
			ip = candidate
			break
		}
	}
	if ip == nil {
		return Node{}, errors.New("missing IPv4 address")
	}
	node := newNode(values["id"], name, capabilities, ip, entry.Port)
	if rawMax, ok := values["target_count_max"]; ok {
		maxTargets, err := strconv.Atoi(rawMax)
		if err != nil || maxTargets < 1 {
			return Node{}, fmt.Errorf("invalid target_count_max %q", rawMax)
		}
		if err := node.setCapabilityMetadata(CapabilityTargetCount, TargetCountCapability{Max: maxTargets}); err != nil {
			return Node{}, fmt.Errorf("invalid target_count_max: %w", err)
		}
	}
	return node, nil
}

func parseCapabilities(value string) []Capability {
	seen := make(map[Capability]bool)
	var capabilities []Capability
	for _, raw := range strings.Split(value, ",") {
		capability := Capability(strings.TrimSpace(raw))
		if capability != "" && !seen[capability] {
			seen[capability] = true
			capabilities = append(capabilities, capability)
		}
	}
	sort.Slice(capabilities, func(i, j int) bool { return capabilities[i] < capabilities[j] })
	return capabilities
}
