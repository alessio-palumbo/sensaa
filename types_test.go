package sensaa

import (
	"net"
	"testing"
)

type unrelatedCapabilityMetadata struct{}

func (unrelatedCapabilityMetadata) capabilityMetadata() {}

func TestTargetCountCapability(t *testing.T) {
	node := newNode(
		"node-1",
		"Test",
		[]Capability{CapabilityPresence, CapabilityTargetCount},
		net.ParseIP("192.0.2.1"),
		8765,
	)
	if err := node.setCapabilityMetadata(CapabilityTargetCount, TargetCountCapability{Max: 3}); err != nil {
		t.Fatal(err)
	}

	metadata, ok := node.TargetCountCapability()
	if !ok || metadata.Max != 3 {
		t.Fatalf("TargetCountCapability = %+v, %t; want Max 3", metadata, ok)
	}
	if !node.HasCapability(CapabilityTargetCount) {
		t.Fatal("target-count metadata replaced capability presence")
	}

	update := Update{Presence: true, Targets: []Target{{}, {}}}
	if got := update.TargetCount(); got != 2 {
		t.Fatalf("current target count = %d, want 2", got)
	}
	if metadata.Max != 3 {
		t.Fatalf("maximum target count changed to %d, want 3", metadata.Max)
	}
}

func TestTargetCountCapabilityWithoutMetadata(t *testing.T) {
	node := newNode(
		"node-1",
		"Test",
		[]Capability{CapabilityTargetCount},
		net.ParseIP("192.0.2.1"),
		8765,
	)
	if !node.HasCapability(CapabilityTargetCount) {
		t.Fatal("target_count capability is absent")
	}
	if metadata, ok := node.TargetCountCapability(); ok {
		t.Fatalf("TargetCountCapability returned unrelated metadata: %+v", metadata)
	}
}

func TestTargetCountCapabilityRejectsUnrelatedMetadataType(t *testing.T) {
	node := newNode(
		"node-1",
		"Test",
		[]Capability{CapabilityTargetCount},
		net.ParseIP("192.0.2.1"),
		8765,
	)
	node.capabilityMetadata[CapabilityTargetCount] = unrelatedCapabilityMetadata{}

	if metadata, ok := node.TargetCountCapability(); ok {
		t.Fatalf("TargetCountCapability returned unrelated metadata: %+v", metadata)
	}
}

func TestSetCapabilityMetadataRequiresCapability(t *testing.T) {
	node := newNode(
		"node-1",
		"Test",
		[]Capability{CapabilityPresence},
		net.ParseIP("192.0.2.1"),
		8765,
	)
	if err := node.setCapabilityMetadata(CapabilityTargetCount, TargetCountCapability{Max: 3}); err == nil {
		t.Fatal("setCapabilityMetadata accepted metadata for an absent capability")
	}
}
