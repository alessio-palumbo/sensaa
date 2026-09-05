package sensaa

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

const protocolVersion = 1

type wireMessage struct {
	Type         string       `json:"type"`
	Version      int          `json:"version,omitempty"`
	ID           string       `json:"id,omitempty"`
	Name         string       `json:"name,omitempty"`
	Capabilities []Capability `json:"capabilities,omitempty"`
	Sequence     uint64       `json:"sequence,omitempty"`
	UptimeMS     uint64       `json:"uptime_ms,omitempty"`
	Presence     bool         `json:"presence"`
	TargetCount  int          `json:"target_count,omitempty"`
	Targets      []wireTarget `json:"targets,omitempty"`
}

type wireTarget struct {
	XMM          int16  `json:"x_mm"`
	YMM          int16  `json:"y_mm"`
	VelocityCMS  int16  `json:"velocity_cm_s"`
	ResolutionMM uint16 `json:"resolution_mm"`
}

func decodeWireMessage(line []byte) (wireMessage, error) {
	var message wireMessage
	if err := json.Unmarshal(line, &message); err != nil {
		return wireMessage{}, fmt.Errorf("decode Sensaa message: %w", err)
	}
	return message, nil
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
	return Update{
		Sequence: message.Sequence,
		Uptime:   time.Duration(message.UptimeMS) * time.Millisecond,
		Presence: message.Presence,
		Targets:  targets,
	}, nil
}

func itoa(value int) string { return strconv.Itoa(value) }
