package sensaa

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"
)

func TestConnectAndRead(t *testing.T) {
	node, stop := serveTestStream(t,
		`{"type":"hello","version":1,"id":"node-1","name":"Test","capabilities":["presence","target_count"],"capability_metadata":{"target_count":{"max":3}}}`+"\n"+
			`{"type":"update","sequence":7,"uptime_ms":1250,"presence":true,"target_count":1,"targets":[{"x_mm":-300,"y_mm":400,"velocity_cm_s":-8,"resolution_mm":360}],"network":{"transport":"wifi","rssi_dbm":-58,"channel":6,"reconnect_count":1}}`+"\n",
	)
	defer stop()
	node.capabilities = []Capability{CapabilityPresence, CapabilityTargetCount}
	node.capabilityMetadata = map[Capability]CapabilityMetadata{
		CapabilityTargetCount: TargetCountCapability{Max: 3},
	}

	client, err := node.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	update, err := client.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if update.Sequence != 7 || update.Uptime != 1250*time.Millisecond || !update.Presence {
		t.Fatalf("unexpected update: %+v", update)
	}
	if update.TargetCount() != 1 {
		t.Fatalf("target count = %d, want 1", update.TargetCount())
	}
	want := Target{PositionMM: PositionMM{X: -300, Y: 400}, VelocityCMS: -8, ResolutionMM: 360}
	if update.Targets[0] != want {
		t.Fatalf("target = %+v, want %+v", update.Targets[0], want)
	}
	if update.Network == nil || update.Network.Transport != NetworkTransportWiFi || update.Network.RSSIDBm == nil || *update.Network.RSSIDBm != -58 {
		t.Fatalf("network telemetry = %+v, want Wi-Fi RSSI -58 dBm", update.Network)
	}
}

func TestConnectRejectsWrongIdentity(t *testing.T) {
	node, stop := serveTestStream(t, `{"type":"hello","version":1,"id":"somebody-else"}`+"\n")
	defer stop()

	_, err := node.Connect(context.Background())
	if err == nil {
		t.Fatal("Connect accepted a different node identity")
	}
}

func TestConnectValidatesTargetCountMetadata(t *testing.T) {
	node, stop := serveTestStream(t, `{"type":"hello","version":1,"id":"node-1","capabilities":["target_count"],"capability_metadata":{"target_count":{"max":2}}}`+"\n")
	defer stop()
	node.capabilities = []Capability{CapabilityTargetCount}
	node.capabilityMetadata = map[Capability]CapabilityMetadata{
		CapabilityTargetCount: TargetCountCapability{Max: 3},
	}

	_, err := node.Connect(context.Background())
	if err == nil {
		t.Fatal("Connect accepted target-count metadata that differed from discovery")
	}
}

func TestConnectAcceptsMissingGreetingMetadata(t *testing.T) {
	node, stop := serveTestStream(t, `{"type":"hello","version":1,"id":"node-1","capabilities":["target_count"]}`+"\n")
	defer stop()
	node.capabilities = []Capability{CapabilityTargetCount}
	node.capabilityMetadata = map[Capability]CapabilityMetadata{
		CapabilityTargetCount: TargetCountCapability{Max: 3},
	}

	client, err := node.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
}

func TestReadHonoursCancellation(t *testing.T) {
	node, stop := serveTestStream(t, `{"type":"hello","version":1,"id":"node-1"}`+"\n")
	defer stop()
	client, err := node.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.Read(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Read error = %v, want context.Canceled", err)
	}
}

func TestReadRejectsInconsistentUpdate(t *testing.T) {
	node, stop := serveTestStream(t,
		`{"type":"hello","version":1,"id":"node-1"}`+"\n"+
			`{"type":"update","presence":false,"target_count":1,"targets":[{"x_mm":0,"y_mm":1}]}`+"\n",
	)
	defer stop()
	client, err := node.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.Read(context.Background()); err == nil {
		t.Fatal("Read accepted an inconsistent update")
	}
}

func serveTestStream(t *testing.T, stream string) (Node, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = io.WriteString(conn, stream)
		<-time.After(250 * time.Millisecond)
	}()
	port := listener.Addr().(*net.TCPAddr).Port
	node := newNode("node-1", "Test", []Capability{CapabilityPresence}, net.ParseIP("127.0.0.1"), port)
	return node, func() {
		_ = listener.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Errorf("test server did not stop on %s", fmt.Sprint(listener.Addr()))
		}
	}
}
