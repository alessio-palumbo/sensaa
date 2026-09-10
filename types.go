package sensaa

import (
	"fmt"
	"net"
	"slices"
	"time"
)

// Capability identifies a kind of information published by a node.
type Capability string

const (
	CapabilityPresence         Capability = "presence"
	CapabilityTargetCount      Capability = "target_count"
	CapabilityTargetPosition   Capability = "target_position"
	CapabilityTargetResolution Capability = "target_resolution"
	CapabilityTargetVelocity   Capability = "target_velocity"
)

// CapabilityMetadata is optional static information describing a capability.
// Implementations are defined by this package so callers use typed accessors
// instead of handling untyped metadata.
type CapabilityMetadata interface {
	capabilityMetadata()
}

// TargetCountCapability describes the limits of a node's target-count
// capability. Max is the maximum number of simultaneous targets the node
// claims it can report.
type TargetCountCapability struct {
	Max int `json:"max"`
}

func (TargetCountCapability) capabilityMetadata() {}

// Node is a discovered Sensaa sensor node.
type Node struct {
	id                 string
	name               string
	capabilities       []Capability
	capabilityMetadata map[Capability]CapabilityMetadata
	address            string
}

func (n Node) ID() string { return n.id }

func (n Node) Name() string { return n.name }

// Capabilities returns a copy of the node's advertised capabilities.
func (n Node) Capabilities() []Capability { return slices.Clone(n.capabilities) }

func (n Node) HasCapability(want Capability) bool {
	return slices.Contains(n.capabilities, want)
}

// TargetCountCapability returns target-count metadata when the node advertises
// it. The boolean is false for nodes without this capability metadata.
func (n Node) TargetCountCapability() (TargetCountCapability, bool) {
	metadata, ok := n.capabilityMetadata[CapabilityTargetCount]
	if !ok {
		return TargetCountCapability{}, false
	}
	targetCount, ok := metadata.(TargetCountCapability)
	return targetCount, ok
}

// PositionMM is a two-dimensional position in millimetres relative to a node.
type PositionMM struct {
	X int16
	Y int16
}

// Target is one target tracked by a node.
type Target struct {
	PositionMM   PositionMM
	VelocityCMS  int16
	ResolutionMM uint16
}

// NetworkTransport identifies the link used by a node to publish updates.
// Unknown future values are preserved for forward compatibility.
type NetworkTransport string

const NetworkTransportWiFi NetworkTransport = "wifi"

// NetworkTelemetry contains optional, point-in-time information about a
// node's network link. Metric pointers are nil when the node cannot report
// that value. ReconnectCount counts reconnections since the node booted.
type NetworkTelemetry struct {
	Transport      NetworkTransport
	RSSIDBm        *int
	Channel        *int
	ReconnectCount *uint32
}

// Update is a complete snapshot of the current sensor state.
type Update struct {
	Sequence uint64
	Uptime   time.Duration
	Presence bool
	Targets  []Target
	Network  *NetworkTelemetry
}

func (u Update) TargetCount() int { return len(u.Targets) }

func newNode(id, name string, capabilities []Capability, ip net.IP, port int) Node {
	return Node{
		id:                 id,
		name:               name,
		capabilities:       slices.Clone(capabilities),
		capabilityMetadata: make(map[Capability]CapabilityMetadata),
		address:            net.JoinHostPort(ip.String(), itoa(port)),
	}
}

func (n *Node) setCapabilityMetadata(capability Capability, metadata CapabilityMetadata) error {
	if !n.HasCapability(capability) {
		return fmt.Errorf("metadata advertised without capability %q", capability)
	}
	if n.capabilityMetadata == nil {
		n.capabilityMetadata = make(map[Capability]CapabilityMetadata)
	}
	n.capabilityMetadata[capability] = metadata
	return nil
}
