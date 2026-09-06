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
