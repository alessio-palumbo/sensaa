package sensaa

import (
	"net"
	"testing"

	"github.com/grandcat/zeroconf"
)

func TestNodeFromEntry(t *testing.T) {
	entry := zeroconf.NewServiceEntry("Room sensor", serviceType, serviceDomain)
	entry.Port = 8765
	entry.Text = []string{
		"ver=1",
		"id=sensaa-aabbccddeeff",
		"name=Living room",
		"caps=target_velocity,presence,target_resolution,target_position,target_count,presence",
		"target_count_max=3",
	}
	entry.AddrIPv4 = []net.IP{net.ParseIP("192.0.2.10")}

	node, err := nodeFromEntry(entry)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := node.ID(), "sensaa-aabbccddeeff"; got != want {
		t.Fatalf("ID = %q, want %q", got, want)
	}
	if got, want := node.Name(), "Living room"; got != want {
		t.Fatalf("Name = %q, want %q", got, want)
	}
	if got, want := node.address, "192.0.2.10:8765"; got != want {
		t.Fatalf("address = %q, want %q", got, want)
	}
	wantCapabilities := []Capability{
		CapabilityPresence,
		CapabilityTargetCount,
		CapabilityTargetPosition,
		CapabilityTargetResolution,
		CapabilityTargetVelocity,
	}
	gotCapabilities := node.Capabilities()
	if len(gotCapabilities) != len(wantCapabilities) {
		t.Fatalf("capabilities = %v, want %v", gotCapabilities, wantCapabilities)
	}
	for i := range wantCapabilities {
		if gotCapabilities[i] != wantCapabilities[i] {
			t.Errorf("capability %d = %q, want %q", i, gotCapabilities[i], wantCapabilities[i])
		}
	}
	gotCapabilities[0] = "changed"
	if node.Capabilities()[0] == "changed" {
		t.Fatal("Capabilities returned node-owned storage")
	}
	targetCount, ok := node.TargetCountCapability()
	if !ok || targetCount.Max != 3 {
		t.Fatalf("TargetCountCapability = %+v, %t; want Max 3", targetCount, ok)
	}
}

func TestNodeFromEntryRejectsIncompleteAdvertisement(t *testing.T) {
	entry := zeroconf.NewServiceEntry("Broken", serviceType, serviceDomain)
	entry.Port = 8765
	entry.AddrIPv4 = []net.IP{net.ParseIP("192.0.2.10")}

	for _, text := range [][]string{
		{"ver=2", "id=node", "caps=presence"},
		{"ver=1", "caps=presence"},
		{"ver=1", "id=node"},
		{"ver=1", "id=node", "caps=target_count", "target_count_max=zero"},
		{"ver=1", "id=node", "caps=presence", "target_count_max=3"},
	} {
		entry.Text = text
		if _, err := nodeFromEntry(entry); err == nil {
			t.Errorf("nodeFromEntry accepted TXT %v", text)
		}
	}
}

func TestNodeFromEntryCapabilityWithoutMetadata(t *testing.T) {
	entry := zeroconf.NewServiceEntry("Presence sensor", serviceType, serviceDomain)
	entry.Port = 8765
	entry.Text = []string{"ver=1", "id=node", "caps=presence,target_count"}
	entry.AddrIPv4 = []net.IP{net.ParseIP("192.0.2.10")}

	node, err := nodeFromEntry(entry)
	if err != nil {
		t.Fatal(err)
	}
	if !node.HasCapability(CapabilityTargetCount) {
		t.Fatal("target_count capability is absent")
	}
	if metadata, ok := node.TargetCountCapability(); ok {
		t.Fatalf("TargetCountCapability = %+v, want unavailable", metadata)
	}
}
