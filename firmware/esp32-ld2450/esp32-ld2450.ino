#include <Arduino.h>
#include <ESPmDNS.h>
#include <NetworkClient.h>
#include <NetworkServer.h>
#include <WiFi.h>
#include <cstdarg>
#include <cstring>

#include "config.h"

HardwareSerial radarSerial(1);

constexpr int RADAR_RX = 4; // connected to LD2450 TX
constexpr int RADAR_TX = 3; // connected to LD2450 RX

constexpr uint16_t SENSAA_PORT = 8765;
constexpr unsigned long WIFI_RETRY_INTERVAL_MS = 10000;
constexpr unsigned long NETWORK_DIAGNOSTIC_INTERVAL_MS = 30000;
constexpr size_t UPDATE_MESSAGE_SIZE = 512;

constexpr uint8_t FRAME_HEADER[] = {0xAA, 0xFF, 0x03, 0x00};
constexpr uint8_t FRAME_FOOTER[] = {0x55, 0xCC};
constexpr size_t TARGET_COUNT = 3;
constexpr size_t TARGET_SIZE = 8;
constexpr size_t FRAME_SIZE = sizeof(FRAME_HEADER) +
                              TARGET_COUNT * TARGET_SIZE +
                              sizeof(FRAME_FOOTER);

struct Target {
    int16_t x;
    int16_t y;
    int16_t speed;
    uint16_t resolution;
    bool valid;
};

uint8_t frame[FRAME_SIZE];
size_t framePosition = 0;
uint64_t sequenceNumber = 0;

NetworkServer sensaaServer(SENSAA_PORT, 1);
NetworkClient sensaaClient;
bool networkServicesRunning = false;
unsigned long lastWiFiAttempt = 0;
unsigned long lastNetworkDiagnostic = 0;
uint32_t wifiConnectionCount = 0;
char nodeID[32];
char hostname[32];

uint16_t readUint16(const uint8_t *data) {
    return static_cast<uint16_t>(data[0]) |
           (static_cast<uint16_t>(data[1]) << 8);
}

// LD2450 coordinates and speed are sign/magnitude values. Bit 15 set means
// positive; bit 15 clear means negative (the opposite of normal int16_t).
int16_t readSignedValue(const uint8_t *data) {
    const uint16_t raw = readUint16(data);
    const int16_t magnitude = raw & 0x7FFF;
    return (raw & 0x8000) ? magnitude : -magnitude;
}

Target decodeTarget(const uint8_t *data) {
    bool valid = false;
    for (size_t i = 0; i < TARGET_SIZE; ++i) {
        valid |= data[i] != 0;
    }

    return {
        readSignedValue(data),
        readSignedValue(data + 2),
        readSignedValue(data + 4),
        readUint16(data + 6),
        valid,
    };
}

size_t decodeTargets(const uint8_t *data, Target *targets) {
    size_t count = 0;
    for (size_t i = 0; i < TARGET_COUNT; ++i) {
        targets[i] = decodeTarget(data + sizeof(FRAME_HEADER) + i * TARGET_SIZE);
        if (targets[i].valid) {
            ++count;
        }
    }
    return count;
}

void printFrame(const Target *targets, size_t count) {
    Serial.print("occupied=");
    Serial.print(count > 0 ? "yes" : "no");
    Serial.print(" targets=");
    Serial.print(count);

    for (size_t i = 0; i < TARGET_COUNT; ++i) {
        if (!targets[i].valid) {
            continue;
        }

        Serial.print(" | T");
        Serial.print(i + 1);
        Serial.print(" x=");
        Serial.print(targets[i].x);
        Serial.print("mm y=");
        Serial.print(targets[i].y);
        Serial.print("mm speed=");
        Serial.print(targets[i].speed);
        Serial.print("cm/s resolution=");
        Serial.print(targets[i].resolution);
        Serial.print("mm");
    }

    Serial.println();
}

void printJSONString(Print &output, const char *value) {
    output.print('"');
    for (const char *cursor = value; *cursor != '\0'; ++cursor) {
        const unsigned char character = static_cast<unsigned char>(*cursor);
        switch (character) {
        case '"': output.print("\\\""); break;
        case '\\': output.print("\\\\"); break;
        case '\n': output.print("\\n"); break;
        case '\r': output.print("\\r"); break;
        case '\t': output.print("\\t"); break;
        default:
            if (character >= 0x20) {
                output.write(character);
            }
        }
    }
    output.print('"');
}

void sendHello(NetworkClient &client) {
    client.print("{\"type\":\"hello\",\"version\":1,\"id\":");
    printJSONString(client, nodeID);
    client.print(",\"name\":");
    printJSONString(client, SENSAA_NODE_NAME);
    client.print(",\"capabilities\":[\"presence\",\"target_count\",\"target_position\",\"target_resolution\",\"target_velocity\"]");
    client.print(",\"capability_metadata\":{\"target_count\":{\"max\":");
    client.print(TARGET_COUNT);
    client.println("}}}");
}

bool appendFormat(char *buffer, size_t capacity, size_t &length, const char *format, ...) {
    if (length >= capacity) {
        return false;
    }

    va_list arguments;
    va_start(arguments, format);
    const int written = vsnprintf(buffer + length, capacity - length, format, arguments);
    va_end(arguments);
    if (written < 0 || static_cast<size_t>(written) >= capacity - length) {
        return false;
    }
    length += static_cast<size_t>(written);
    return true;
}

void publishFrame(const Target *targets, size_t count) {
    if (!sensaaClient || !sensaaClient.connected()) {
        return;
    }

    char message[UPDATE_MESSAGE_SIZE];
    size_t length = 0;
    const uint64_t sequence = ++sequenceNumber;
    bool complete = appendFormat(
        message,
        sizeof(message),
        length,
        "{\"type\":\"update\",\"sequence\":%llu,\"uptime_ms\":%lu,\"presence\":%s,\"target_count\":%u,\"targets\":[",
        static_cast<unsigned long long>(sequence),
        millis(),
        count > 0 ? "true" : "false",
        static_cast<unsigned int>(count));

    bool first = true;
    for (size_t i = 0; complete && i < TARGET_COUNT; ++i) {
        if (!targets[i].valid) {
            continue;
        }
        complete = appendFormat(
            message,
            sizeof(message),
            length,
            "%s{\"x_mm\":%d,\"y_mm\":%d,\"velocity_cm_s\":%d,\"resolution_mm\":%u}",
            first ? "" : ",",
            targets[i].x,
            targets[i].y,
            targets[i].speed,
            targets[i].resolution);
        first = false;
    }
    complete = complete && appendFormat(message, sizeof(message), length, "]}\n");
    if (!complete) {
        Serial.println("Sensaa update exceeded its message buffer");
        return;
    }

    // A complete snapshot is deliberately submitted in one call. This avoids
    // turning the many JSON fields into small TCP writes when TCP_NODELAY is on.
    if (sensaaClient.write(reinterpret_cast<const uint8_t *>(message), length) != length) {
        Serial.println("Sensaa client write failed; closing stream");
        sensaaClient.stop();
    }
}

void handleFrame(const uint8_t *data) {
    Target targets[TARGET_COUNT];
    const size_t count = decodeTargets(data, targets);
    printFrame(targets, count);
    publishFrame(targets, count);
}

bool hasValidFooter(const uint8_t *data) {
    return data[FRAME_SIZE - 2] == FRAME_FOOTER[0] &&
           data[FRAME_SIZE - 1] == FRAME_FOOTER[1];
}

// Preserve a partial header found inside a malformed frame. This lets the
// parser recover quickly if a UART byte was dropped or inserted.
void resynchroniseFrame() {
    // Prefer the newest complete header in the buffered bytes.
    for (size_t start = FRAME_SIZE - sizeof(FRAME_HEADER); start > 0; --start) {
        if (memcmp(frame + start, FRAME_HEADER, sizeof(FRAME_HEADER)) == 0) {
            const size_t remaining = FRAME_SIZE - start;
            memmove(frame, frame + start, remaining);
            framePosition = remaining;
            return;
        }
    }

    // Otherwise retain only a possible partial header at the end.
    for (size_t length = sizeof(FRAME_HEADER) - 1; length > 0; --length) {
        if (memcmp(frame + FRAME_SIZE - length, FRAME_HEADER, length) == 0) {
            memmove(frame, frame + FRAME_SIZE - length, length);
            framePosition = length;
            return;
        }
    }

    framePosition = 0;
}

void processRadarByte(uint8_t value) {
    if (framePosition < sizeof(FRAME_HEADER)) {
        if (value == FRAME_HEADER[framePosition]) {
            frame[framePosition++] = value;
        } else {
            // AA can be both the mismatched byte and the start of a new header.
            framePosition = value == FRAME_HEADER[0] ? 1 : 0;
            if (framePosition == 1) {
                frame[0] = value;
            }
        }
        return;
    }

    frame[framePosition++] = value;
    if (framePosition < FRAME_SIZE) {
        return;
    }

    if (hasValidFooter(frame)) {
        handleFrame(frame);
        framePosition = 0;
    } else {
        resynchroniseFrame();
    }
}

void stopNetworkServices() {
    if (!networkServicesRunning) {
        return;
    }
    sensaaClient.stop();
    sensaaServer.end();
    MDNS.end();
    networkServicesRunning = false;
    Serial.println("Sensaa LAN service stopped");
}

void startNetworkServices() {
    sensaaServer.begin();
    sensaaServer.setNoDelay(true);

    if (!MDNS.begin(hostname)) {
        Serial.println("Could not start mDNS; will retry after Wi-Fi reconnect");
        sensaaServer.end();
        return;
    }
    MDNS.setInstanceName(SENSAA_NODE_NAME);
    MDNS.addService("sensaa", "tcp", SENSAA_PORT);
    MDNS.addServiceTxt("sensaa", "tcp", "ver", "1");
    MDNS.addServiceTxt("sensaa", "tcp", "id", static_cast<const char *>(nodeID));
    MDNS.addServiceTxt("sensaa", "tcp", "name", SENSAA_NODE_NAME);
    MDNS.addServiceTxt("sensaa", "tcp", "caps", "presence,target_count,target_position,target_resolution,target_velocity");
    char targetCountMax[12];
    snprintf(targetCountMax, sizeof(targetCountMax), "%u", static_cast<unsigned int>(TARGET_COUNT));
    MDNS.addServiceTxt("sensaa", "tcp", "target_count_max", static_cast<const char *>(targetCountMax));

    networkServicesRunning = true;
    ++wifiConnectionCount;
    lastNetworkDiagnostic = millis();
    Serial.print("Sensaa node ");
    Serial.print(nodeID);
    Serial.print(" listening at ");
    Serial.print(WiFi.localIP());
    Serial.print(':');
    Serial.print(SENSAA_PORT);
    Serial.print(" RSSI=");
    Serial.print(WiFi.RSSI());
    Serial.print("dBm channel=");
    Serial.println(WiFi.channel());
}

void printNetworkDiagnostic() {
    const unsigned long now = millis();
    if (!networkServicesRunning || now - lastNetworkDiagnostic < NETWORK_DIAGNOSTIC_INTERVAL_MS) {
        return;
    }
    lastNetworkDiagnostic = now;
    Serial.print("Sensaa network RSSI=");
    Serial.print(WiFi.RSSI());
    Serial.print("dBm channel=");
    Serial.print(WiFi.channel());
    Serial.print(" reconnects=");
    Serial.print(wifiConnectionCount > 0 ? wifiConnectionCount - 1 : 0);
    Serial.print(" client=");
    Serial.print(sensaaClient && sensaaClient.connected() ? "connected" : "none");
    Serial.print(" sequence=");
    Serial.printf("%llu\n", static_cast<unsigned long long>(sequenceNumber));
}

void maintainNetwork() {
    if (WiFi.status() != WL_CONNECTED) {
        stopNetworkServices();
        const unsigned long now = millis();
        if (now - lastWiFiAttempt >= WIFI_RETRY_INTERVAL_MS) {
            lastWiFiAttempt = now;
            Serial.print("Connecting to Wi-Fi ");
            Serial.println(SENSAA_WIFI_SSID);
            WiFi.disconnect();
            WiFi.begin(SENSAA_WIFI_SSID, SENSAA_WIFI_PASSWORD);
        }
        return;
    }

    if (!networkServicesRunning) {
        startNetworkServices();
    }
    if (!networkServicesRunning) {
        return;
    }

    if (!sensaaClient.connected()) {
        sensaaClient.stop();
        NetworkClient incoming = sensaaServer.accept();
        if (incoming) {
            sensaaClient = incoming;
            sensaaClient.setNoDelay(true);
            sendHello(sensaaClient);
            Serial.print("Sensaa client connected from ");
            Serial.println(sensaaClient.remoteIP());
        }
    }
    printNetworkDiagnostic();
}

void setup() {
    Serial.begin(115200);
    delay(1000);

    Serial.println("Sensaa ESP32-LD2450 node starting...");

    // LD2450 UART: 256000 baud, 8 data bits, no parity, 1 stop bit.
    radarSerial.begin(256000, SERIAL_8N1, RADAR_RX, RADAR_TX);
    Serial.println("LD2450 UART ready. Waiting for target frames...");

    const uint64_t mac = ESP.getEfuseMac();
    snprintf(nodeID, sizeof(nodeID), "sensaa-%012llx", static_cast<unsigned long long>(mac));
    snprintf(hostname, sizeof(hostname), "sensaa-%06lx", static_cast<unsigned long>(mac & 0xFFFFFF));

    WiFi.mode(WIFI_STA);
    if (!WiFi.setSleep(false)) {
        Serial.println("Could not disable Wi-Fi modem sleep");
    }
    WiFi.setHostname(hostname);
    // Make the first retry eligible immediately without blocking radar input.
    lastWiFiAttempt = millis() - WIFI_RETRY_INTERVAL_MS;
}

void loop() {
    while (radarSerial.available()) {
        processRadarByte(static_cast<uint8_t>(radarSerial.read()));
    }
    maintainNetwork();
}
