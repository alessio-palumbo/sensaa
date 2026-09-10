package sensaa

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"time"
)

const protocolVersion = 1

type wireMessage struct {
	Type               string                         `json:"type"`
	Version            int                            `json:"version,omitempty"`
	ID                 string                         `json:"id,omitempty"`
	Name               string                         `json:"name,omitempty"`
	Capabilities       []Capability                   `json:"capabilities,omitempty"`
	CapabilityMetadata map[Capability]json.RawMessage `json:"capability_metadata,omitempty"`
	Sequence           uint64                         `json:"sequence,omitempty"`
	UptimeMS           uint64                         `json:"uptime_ms,omitempty"`
	Presence           bool                           `json:"presence"`
	TargetCount        int                            `json:"target_count,omitempty"`
	Targets            []wireTarget                   `json:"targets,omitempty"`
	Network            *wireNetworkTelemetry          `json:"network,omitempty"`
}

type wireTarget struct {
	XMM          int16  `json:"x_mm"`
	YMM          int16  `json:"y_mm"`
	VelocityCMS  int16  `json:"velocity_cm_s"`
	ResolutionMM uint16 `json:"resolution_mm"`
}

type wireNetworkTelemetry struct {
	Transport      NetworkTransport `json:"transport"`
	RSSIDBm        *int             `json:"rssi_dbm,omitempty"`
	Channel        *int             `json:"channel,omitempty"`
	ReconnectCount *uint32          `json:"reconnect_count,omitempty"`
}

func decodeWireMessage(line []byte) (wireMessage, error) {
	var message wireMessage
	if err := json.Unmarshal(line, &message); err != nil {
		return wireMessage{}, fmt.Errorf("decode Sensaa message: %w", err)
	}
	return message, nil
}

func capabilityMetadataFromWire(message wireMessage) (map[Capability]CapabilityMetadata, error) {
	metadata := make(map[Capability]CapabilityMetadata)
	for capability, raw := range message.CapabilityMetadata {
		switch capability {
		case CapabilityTargetCount:
			if !slices.Contains(message.Capabilities, capability) {
				return nil, fmt.Errorf("metadata provided without capability %q", capability)
			}
			var targetCount TargetCountCapability
			if err := json.Unmarshal(raw, &targetCount); err != nil {
				return nil, fmt.Errorf("decode %s capability metadata: %w", capability, err)
			}
			if targetCount.Max < 1 {
				return nil, fmt.Errorf("invalid %s capability maximum %d", capability, targetCount.Max)
			}
			metadata[capability] = targetCount
		default:
			// Preserve forward compatibility: retain the advertised capability
			// name, but ignore metadata this library version cannot type safely.
		}
	}
	return metadata, nil
}

func updateFromWire(message wireMessage) (Update, error) {
	if message.Type != "update" {
		return Update{}, fmt.Errorf("unexpected Sensaa message type %q", message.Type)
	}
	if message.TargetCount != len(message.Targets) {
		return Update{}, fmt.Errorf("target_count is %d but update contains %d targets", message.TargetCount, len(message.Targets))
	}
	if message.Presence != (message.TargetCount > 0) {
		return Update{}, fmt.Errorf("presence conflicts with target_count %d", message.TargetCount)
	}
	targets := make([]Target, len(message.Targets))
	for i, target := range message.Targets {
		targets[i] = Target{
			PositionMM:   PositionMM{X: target.XMM, Y: target.YMM},
			VelocityCMS:  target.VelocityCMS,
			ResolutionMM: target.ResolutionMM,
		}
	}
	network, err := networkTelemetryFromWire(message.Network)
	if err != nil {
		return Update{}, err
	}
	return Update{
		Sequence: message.Sequence,
		Uptime:   time.Duration(message.UptimeMS) * time.Millisecond,
		Presence: message.Presence,
		Targets:  targets,
		Network:  network,
	}, nil
}

func networkTelemetryFromWire(wire *wireNetworkTelemetry) (*NetworkTelemetry, error) {
	if wire == nil {
		return nil, nil
	}
	if wire.Transport == "" {
		return nil, fmt.Errorf("network telemetry is missing its transport")
	}
	return &NetworkTelemetry{
		Transport:      wire.Transport,
		RSSIDBm:        wire.RSSIDBm,
		Channel:        wire.Channel,
		ReconnectCount: wire.ReconnectCount,
	}, nil
}

func itoa(value int) string { return strconv.Itoa(value) }
