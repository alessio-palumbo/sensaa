package sensaa

import (
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

// Node is a discovered Sensaa sensor node.
type Node struct {
	id           string
	name         string
	capabilities []Capability
	address      string
}

func (n Node) ID() string { return n.id }

func (n Node) Name() string { return n.name }

// Capabilities returns a copy of the node's advertised capabilities.
func (n Node) Capabilities() []Capability { return slices.Clone(n.capabilities) }

func (n Node) HasCapability(want Capability) bool {
	return slices.Contains(n.capabilities, want)
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

// Update is a complete snapshot of the current sensor state.
type Update struct {
	Sequence uint64
	Uptime   time.Duration
	Presence bool
	Targets  []Target
}

func (u Update) TargetCount() int { return len(u.Targets) }

func newNode(id, name string, capabilities []Capability, ip net.IP, port int) Node {
	return Node{
		id:           id,
		name:         name,
		capabilities: slices.Clone(capabilities),
		address:      net.JoinHostPort(ip.String(), itoa(port)),
	}
}
