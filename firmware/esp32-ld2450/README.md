# ESP32-C3 + LD2450 node

Wiring:

| LD2450 | ESP32-C3 SuperMini |
| --- | --- |
| TX | GPIO4 |
| RX | GPIO3 |
| VCC | 5V |
| GND | GND |

Copy `config.example.h` to `config.h`, set the Wi-Fi credentials and friendly
node name, then compile and upload from the repository root:

```sh
arduino-cli compile \
  --fqbn esp32:esp32:esp32c3:CDCOnBoot=cdc \
  firmware/esp32-ld2450

arduino-cli upload \
  --fqbn esp32:esp32:esp32c3:CDCOnBoot=cdc \
  --port /dev/cu.usbmodemXXXX \
  firmware/esp32-ld2450
```

USB serial at 115200 baud remains available for diagnostics. Normal sensor
streaming uses Wi-Fi, mDNS/DNS-SD service `_sensaa._tcp.local`, and TCP port
8765; USB does not need to remain connected after flashing. The firmware
disables Wi-Fi modem sleep to favour predictable LAN latency over power use,
which is appropriate for this USB-powered node. Every 30 seconds, serial also
reports RSSI, channel, reconnect count, client state, and stream sequence to
help distinguish radio problems from application latency.
