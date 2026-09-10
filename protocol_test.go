package sensaa

import (
	"encoding/json"
	"testing"
)

func TestGreetingCapabilityMetadataRoundTrip(t *testing.T) {
	want := wireMessage{
		Type:         "hello",
		Version:      protocolVersion,
		ID:           "node-1",
		Capabilities: []Capability{CapabilityPresence, CapabilityTargetCount},
		CapabilityMetadata: map[Capability]json.RawMessage{
			CapabilityTargetCount: json.RawMessage(`{"max":3}`),
		},
	}

	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeWireMessage(append(encoded, '\n'))
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := capabilityMetadataFromWire(decoded)
	if err != nil {
		t.Fatal(err)
	}
	targetCount, ok := metadata[CapabilityTargetCount].(TargetCountCapability)
	if !ok || targetCount.Max != 3 {
		t.Fatalf("decoded metadata = %#v; want target_count Max 3", metadata)
	}
}

func TestGreetingWithoutCapabilityMetadataIsBackwardCompatible(t *testing.T) {
	message, err := decodeWireMessage([]byte(
		`{"type":"hello","version":1,"id":"node-1","capabilities":["presence","target_count"]}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := capabilityMetadataFromWire(message)
	if err != nil {
		t.Fatal(err)
	}
	if len(metadata) != 0 {
		t.Fatalf("metadata = %#v, want none", metadata)
	}
}

func TestUnknownGreetingCapabilityMetadataIsIgnored(t *testing.T) {
	message, err := decodeWireMessage([]byte(
		`{"type":"hello","version":1,"id":"node-1","capabilities":["future_sensor"],"capability_metadata":{"future_sensor":{"range":10}}}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := capabilityMetadataFromWire(message)
	if err != nil {
		t.Fatal(err)
	}
	if len(metadata) != 0 {
		t.Fatalf("metadata = %#v, want unknown metadata ignored", metadata)
	}
}

func TestTargetCountWireMetadataRequiresCapability(t *testing.T) {
	message, err := decodeWireMessage([]byte(
		`{"type":"hello","version":1,"id":"node-1","capabilities":["presence"],"capability_metadata":{"target_count":{"max":3}}}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := capabilityMetadataFromWire(message); err == nil {
		t.Fatal("capabilityMetadataFromWire accepted metadata for an absent capability")
	}
}

func TestUpdateNetworkTelemetry(t *testing.T) {
	message, err := decodeWireMessage([]byte(
		`{"type":"update","sequence":9,"uptime_ms":2500,"presence":false,"network":{"transport":"wifi","rssi_dbm":-58,"channel":6,"reconnect_count":2}}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	update, err := updateFromWire(message)
	if err != nil {
		t.Fatal(err)
	}
	if update.Network == nil {
		t.Fatal("network telemetry is nil")
	}
	if got := update.Network.Transport; got != NetworkTransportWiFi {
		t.Fatalf("transport = %q, want %q", got, NetworkTransportWiFi)
	}
	if update.Network.RSSIDBm == nil || *update.Network.RSSIDBm != -58 {
		t.Fatalf("RSSI = %v, want -58", update.Network.RSSIDBm)
	}
	if update.Network.Channel == nil || *update.Network.Channel != 6 {
		t.Fatalf("channel = %v, want 6", update.Network.Channel)
	}
	if update.Network.ReconnectCount == nil || *update.Network.ReconnectCount != 2 {
		t.Fatalf("reconnect count = %v, want 2", update.Network.ReconnectCount)
	}
}

func TestUpdateWithoutNetworkTelemetryIsBackwardCompatible(t *testing.T) {
	message, err := decodeWireMessage([]byte(
		`{"type":"update","presence":false}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	update, err := updateFromWire(message)
	if err != nil {
		t.Fatal(err)
	}
	if update.Network != nil {
		t.Fatalf("network telemetry = %#v, want nil", update.Network)
	}
}

func TestUpdateRejectsInvalidNetworkTelemetry(t *testing.T) {
	input := `{"type":"update","presence":false,"network":{"rssi_dbm":-58}}`
	message, err := decodeWireMessage([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := updateFromWire(message); err == nil {
		t.Fatalf("updateFromWire accepted telemetry without a transport: %s", input)
	}
}
