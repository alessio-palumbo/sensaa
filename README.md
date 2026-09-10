# Sensaa

Sensaa is a small LAN sensor protocol and Go client. Sensor hardware publishes
capabilities and structured updates; consuming programs discover nodes without
knowing their IP address or hardware model.

This first vertical slice contains:

- ESP32-C3 + HLK-LD2450 firmware;
- DNS-SD/mDNS discovery at `_sensaa._tcp.local`;
- a versioned newline-delimited JSON stream over TCP;
- a small Go discovery/client API; and
- a LIFX presence demo built on the public API.

## Go API

```go
nodes, err := sensaa.Discover(ctx)
if err != nil {
    return err
}
for _, node := range nodes {
    fmt.Printf("%s: %v\n", node.Name(), node.Capabilities())
    if targetCount, ok := node.TargetCountCapability(); ok {
        fmt.Printf("tracks up to %d targets\n", targetCount.Max)
    }
}

client, err := nodes[0].Connect(ctx)
if err != nil {
    return err
}
defer client.Close()

for {
    update, err := client.Read(ctx)
    if err != nil {
        return err
    }
    fmt.Println(update.Presence, update.TargetCount(), update.Targets)
}
```

`Discover` collects mDNS responses for up to two seconds. `Connect` validates
the versioned node greeting with a five-second handshake limit. `Read` returns
complete typed snapshots and honors context cancellation/deadlines.
Applications can reconnect by rediscovering; the LIFX example does this
automatically and treats five seconds without an update as a stale stream.
Updates may also include optional `NetworkTelemetry`, such as a Wi-Fi RSSI,
channel, and reconnect count. Network metrics describe transport health rather
than sensor capabilities, and consumers must tolerate nodes that omit them.

## Firmware setup

The reference hardware is an ESP32-C3 SuperMini wired as follows:

| LD2450 | ESP32-C3 |
| --- | --- |
| TX | GPIO4 |
| RX | GPIO3 |
| VCC | 5V |
| GND | GND |

Create the ignored local configuration and edit it:

```sh
cp firmware/esp32-ld2450/config.example.h firmware/esp32-ld2450/config.h
```

Set `SENSAA_WIFI_SSID`, `SENSAA_WIFI_PASSWORD`, and `SENSAA_NODE_NAME`, then
connect the ESP32 and find its upload port:

```sh
arduino-cli board list
```

Compile and flash with the tested board settings:

```sh
arduino-cli compile \
  --fqbn esp32:esp32:esp32c3:CDCOnBoot=cdc \
  firmware/esp32-ld2450

arduino-cli upload \
  --fqbn esp32:esp32:esp32c3:CDCOnBoot=cdc \
  --port /dev/cu.usbmodemXXXX \
  firmware/esp32-ld2450
```

Optional diagnostics remain available at 115200 baud:

```sh
arduino-cli monitor --port /dev/cu.usbmodemXXXX --config baudrate=115200
```

Each physical node has two distinct identifiers:

- `Name` is the human-readable, changeable value configured with
  `SENSAA_NODE_NAME`, such as `bedroom-radar`.
- `ID` is the stable machine identity derived from the ESP32's full eFuse MAC
  and exposed through both DNS-SD discovery and the protocol greeting.

The hostname and IP address are connection details, not identity. Discovery
deduplicates nodes by ID, and the client verifies that a connected node's
greeting has the ID that was discovered. Tracked radar targets intentionally
do not receive stable IDs. Wi-Fi credentials live only in ignored `config.h`.

## LIFX experiment

With the flashed sensor and Mac on the same LAN, run:

```sh
go run ./examples/lifx-presence --target all
```

No sensor serial port or IP address is required. Count mode reproduces the
prototype behavior: no target powers off; one, two, or three targets select
warm white, blue, or purple. Distance-driven brightness is available with:

```sh
go run ./examples/lifx-presence --target all --mode distance
```

The temperature experiments are also retained:

```sh
go run ./examples/lifx-presence \
  --target all \
  --mode distance \
  --temperature \
  --temperature-brightness 30
```

If multiple Sensaa nodes are present, the example deterministically uses the
first discovered node. Select one by its advertised name or ID with `--sensor`.

## Minimal protocol v1

DNS-SD TXT records contain `ver`, `id`, `name`, comma-separated `caps`, and
capability-scoped metadata such as `target_count_max`. After a TCP connection
the node sends one `hello` JSON line—including the same capability metadata—
followed by `update` lines. Each update is a complete snapshot containing
presence, target count, and zero or more targets with millimetre positions,
cm/s velocity, and millimetre resolution. An update can additionally carry an
optional `network` object with point-in-time link telemetry. Protocol JSON is
intentionally private to the Go package; callers consume typed values.

The metadata returned by `Node` is the discovery snapshot. On connection,
metadata present in both discovery and the greeting must agree. A greeting
without metadata remains compatible with nodes from before metadata was added;
it does not remove metadata already learned during discovery. Unknown future
metadata is ignored until that Sensaa library version has a typed API for it.

This is local-LAN prototype transport with no authentication or encryption.
mDNS generally stays within one multicast/broadcast domain, so guest Wi-Fi,
client isolation, VLANs, VPN routing, or multicast-filtering access points can
prevent discovery.
