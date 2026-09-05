package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/alessio-palumbo/lifxlan-go/pkg/controller"
	"github.com/alessio-palumbo/lifxlan-go/pkg/device"
	"github.com/alessio-palumbo/lifxlan-go/pkg/messages"
	"github.com/alessio-palumbo/sensaa"
)

const (
	brightnessStep               = 5
	temperatureStep              = 100
	defaultBrightness            = 70
	defaultTempBrightness        = 30
	warmTemperature       uint16 = 2700
	midTemperature        uint16 = 4200
	coolTemperature       uint16 = 6500
)

type config struct {
	sensor         string
	target         string
	mode           string
	temperature    bool
	tempBrightness float64
	stableFrames   int
	discoveryWait  time.Duration
	transition     time.Duration
	retryWait      time.Duration
	streamTimeout  time.Duration
}

type lightState struct {
	key        string
	name       string
	targets    int
	poweredOn  bool
	hue        float64
	saturation float64
	brightness float64
	kelvin     uint16
	distanceM  float64
}

type stabilizer struct {
	required       int
	candidateKey   string
	candidateCount int
	currentKey     string
	hasCurrent     bool
}

func main() {
	cfg, err := parseFlags()
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}

func parseFlags() (config, error) {
	var cfg config
	flag.StringVar(&cfg.sensor, "sensor", "", "optional Sensaa node name or ID (default: first discovered node)")
	flag.StringVar(&cfg.target, "target", "all", "LIFX selector (label, group, location, serial, or all)")
	flag.StringVar(&cfg.mode, "mode", "count", "visual mode: count or distance")
	flag.BoolVar(&cfg.temperature, "temperature", false, "use fixed brightness and vary white temperature")
	flag.Float64Var(&cfg.tempBrightness, "temperature-brightness", defaultTempBrightness, "brightness percentage used with --temperature")
	flag.IntVar(&cfg.stableFrames, "stable-frames", 3, "consecutive matching frames required before changing the light")
	flag.DurationVar(&cfg.discoveryWait, "lifx-discovery-wait", 2*time.Second, "time to collect LIFX discovery responses")
	flag.DurationVar(&cfg.transition, "transition", 300*time.Millisecond, "LIFX colour and power transition duration")
	flag.DurationVar(&cfg.retryWait, "retry-wait", 2*time.Second, "delay before rediscovering after a sensor connection failure")
	flag.DurationVar(&cfg.streamTimeout, "stream-timeout", 5*time.Second, "reconnect if a sensor sends no update for this long")
	flag.Parse()

	if strings.TrimSpace(cfg.target) == "" {
		return config{}, errors.New("--target must not be empty")
	}
	if cfg.mode != "count" && cfg.mode != "distance" {
		return config{}, fmt.Errorf("invalid --mode %q: want count or distance", cfg.mode)
	}
	if cfg.tempBrightness < 1 || cfg.tempBrightness > 100 {
		return config{}, errors.New("--temperature-brightness must be between 1 and 100")
	}
	if cfg.stableFrames < 1 {
		return config{}, errors.New("--stable-frames must be at least 1")
	}
	if cfg.discoveryWait < 0 || cfg.transition < 0 || cfg.retryWait < 0 || cfg.streamTimeout <= 0 {
		return config{}, errors.New("durations must be non-negative and --stream-timeout must be positive")
	}
	return cfg, nil
}

func run(ctx context.Context, cfg config) error {
	ctrl, err := controller.New()
	if err != nil {
		return fmt.Errorf("start LIFX controller: %w", err)
	}
	defer ctrl.Close()

	log.Printf("discovering LIFX devices for %s", cfg.discoveryWait)
	if err := wait(ctx, cfg.discoveryWait); err != nil {
		return err
	}
	devices := ctrl.GetDevices()
	serials := device.ResolveSelectorSerials(cfg.target, devices)
	if len(serials) == 0 {
		return fmt.Errorf("LIFX target %q matched no devices (discovered: %s)", cfg.target, describeDevices(devices))
	}
	for _, serialNumber := range serials {
		log.Printf("selected LIFX target %s", describeDevice(serialNumber, devices))
	}

	stable := stabilizer{required: cfg.stableFrames}
	var applied *lightState
	for ctx.Err() == nil {
		node, err := discoverNode(ctx, cfg.sensor)
		if err != nil {
			log.Printf("Sensaa discovery: %v", err)
			if err := wait(ctx, cfg.retryWait); err != nil {
				return err
			}
			continue
		}
		if err := supportsExperiment(node, cfg.mode); err != nil {
			log.Printf("Sensaa node %q: %v", node.Name(), err)
			if err := wait(ctx, cfg.retryWait); err != nil {
				return err
			}
			continue
		}
		log.Printf("connecting to Sensaa node %q (%s), capabilities=%v", node.Name(), node.ID(), node.Capabilities())
		client, err := node.Connect(ctx)
		if err != nil {
			log.Printf("Sensaa connection: %v", err)
			if err := wait(ctx, cfg.retryWait); err != nil {
				return err
			}
			continue
		}

		err = consumeUpdates(ctx, client, ctrl, serials, cfg, &stable, &applied)
		_ = client.Close()
		if ctx.Err() != nil {
			return nil
		}
		log.Printf("Sensaa stream ended: %v; rediscovering", err)
		if err := wait(ctx, cfg.retryWait); err != nil {
			return err
		}
	}
	return nil
}

func supportsExperiment(node sensaa.Node, mode string) error {
	required := []sensaa.Capability{sensaa.CapabilityPresence, sensaa.CapabilityTargetCount}
	if mode == "distance" {
		required = append(required, sensaa.CapabilityTargetPosition)
	}
	for _, capability := range required {
		if !node.HasCapability(capability) {
			return fmt.Errorf("missing required capability %q", capability)
		}
	}
	return nil
}

func discoverNode(ctx context.Context, selector string) (sensaa.Node, error) {
	nodes, err := sensaa.Discover(ctx)
	if err != nil {
		return sensaa.Node{}, err
	}
	if len(nodes) == 0 {
		return sensaa.Node{}, errors.New("no nodes found")
	}
	if selector == "" {
		return nodes[0], nil
	}
	for _, node := range nodes {
		if node.ID() == selector || strings.EqualFold(node.Name(), selector) {
			return node, nil
		}
	}
	return sensaa.Node{}, fmt.Errorf("node %q not found", selector)
}

func consumeUpdates(
	ctx context.Context,
	client *sensaa.Client,
	ctrl *controller.Controller,
	serials []device.Serial,
	cfg config,
	stable *stabilizer,
	applied **lightState,
) error {
	for {
		readCtx, cancel := context.WithTimeout(ctx, cfg.streamTimeout)
		update, err := client.Read(readCtx)
		cancel()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return errors.New("node closed the connection")
			}
			return err
		}
		state, err := stateForUpdate(cfg.mode, cfg.temperature, cfg.tempBrightness, update)
		if err != nil {
			log.Printf("ignoring sensor update: %v", err)
			continue
		}
		if !stable.observe(state.key) {
			continue
		}
		if err := applyLightState(ctrl, serials, state, cfg.transition); err != nil {
			return err
		}
		stable.commit(state.key)
		if *applied == nil || (*applied).targets != state.targets {
			log.Printf("targets=%d", state.targets)
		}
		appliedState := state
		*applied = &appliedState
		if state.poweredOn && cfg.mode == "distance" {
			log.Printf("light=%s brightness=%.0f%% kelvin=%d nearest=%.2fm", state.name, state.brightness, state.kelvin, state.distanceM)
		} else {
			log.Printf("light=%s", state.name)
		}
	}
}

func stateForUpdate(mode string, temperature bool, tempBrightness float64, update sensaa.Update) (lightState, error) {
	count := update.TargetCount()
	if !update.Presence {
		return lightState{key: "off", name: "off"}, nil
	}
	if mode == "distance" {
		distanceM, err := nearestDistanceMetres(update.Targets)
		if err != nil {
			return lightState{}, err
		}
		if temperature {
			kelvin := quantizeTemperature(distanceTemperature(distanceM))
			return lightState{key: fmt.Sprintf("distance-temperature:%d", kelvin), name: "white", targets: count, poweredOn: true, brightness: tempBrightness, kelvin: kelvin, distanceM: distanceM}, nil
		}
		brightness := quantizeBrightness(distanceBrightness(distanceM))
		return lightState{key: fmt.Sprintf("distance:%.0f", brightness), name: "warm white", targets: count, poweredOn: true, brightness: brightness, kelvin: warmTemperature, distanceM: distanceM}, nil
	}
	if temperature {
		return countTemperatureState(count, tempBrightness)
	}
	switch count {
	case 1:
		return lightState{key: "count:1", name: "warm white", targets: 1, poweredOn: true, brightness: defaultBrightness, kelvin: warmTemperature}, nil
	case 2:
		return lightState{key: "count:2", name: "blue", targets: 2, poweredOn: true, hue: 220, saturation: 100, brightness: defaultBrightness, kelvin: 3500}, nil
	case 3:
		return lightState{key: "count:3", name: "purple", targets: 3, poweredOn: true, hue: 280, saturation: 90, brightness: defaultBrightness, kelvin: 3500}, nil
	default:
		return lightState{}, fmt.Errorf("unsupported target count %d", count)
	}
}

func countTemperatureState(count int, brightness float64) (lightState, error) {
	kelvinByCount := map[int]uint16{1: coolTemperature, 2: midTemperature, 3: warmTemperature}
	kelvin, ok := kelvinByCount[count]
	if !ok {
		return lightState{}, fmt.Errorf("unsupported target count %d", count)
	}
	return lightState{key: fmt.Sprintf("count-temperature:%d", kelvin), name: "white", targets: count, poweredOn: true, brightness: brightness, kelvin: kelvin}, nil
}

func nearestDistanceMetres(targets []sensaa.Target) (float64, error) {
	if len(targets) == 0 {
		return 0, errors.New("occupied update has no target positions")
	}
	nearestMM := math.Inf(1)
	for _, target := range targets {
		nearestMM = min(nearestMM, math.Hypot(float64(target.PositionMM.X), float64(target.PositionMM.Y)))
	}
	return nearestMM / 1000, nil
}

func distanceBrightness(distanceM float64) float64 {
	switch {
	case distanceM <= 0.5:
		return 100
	case distanceM <= 1.5:
		return 100 - (distanceM-0.5)*30
	case distanceM < 3:
		return 70 - (distanceM-1.5)*(40/1.5)
	default:
		return 30
	}
}

func distanceTemperature(distanceM float64) float64 {
	switch {
	case distanceM <= 0.5:
		return float64(warmTemperature)
	case distanceM <= 1.5:
		return float64(warmTemperature) + (distanceM-0.5)*float64(midTemperature-warmTemperature)
	case distanceM < 3:
		return float64(midTemperature) + (distanceM-1.5)*float64(coolTemperature-midTemperature)/1.5
	default:
		return float64(coolTemperature)
	}
}

func quantizeBrightness(value float64) float64 {
	return math.Round(value/brightnessStep) * brightnessStep
}

func quantizeTemperature(value float64) uint16 {
	steps := math.Round((value - float64(warmTemperature)) / temperatureStep)
	quantized := float64(warmTemperature) + steps*temperatureStep
	return uint16(max(float64(warmTemperature), min(quantized, float64(coolTemperature))))
}

func (s *stabilizer) observe(key string) bool {
	if key != s.candidateKey {
		s.candidateKey, s.candidateCount = key, 1
	} else {
		s.candidateCount++
	}
	return s.candidateCount >= s.required && (!s.hasCurrent || key != s.currentKey)
}

func (s *stabilizer) commit(key string) { s.currentKey, s.hasCurrent = key, true }

func applyLightState(ctrl *controller.Controller, serials []device.Serial, state lightState, transition time.Duration) error {
	for _, serialNumber := range serials {
		if !state.poweredOn {
			if err := ctrl.Send(serialNumber, messages.SetPowerOff(transition)); err != nil {
				return fmt.Errorf("turn off %s: %w", serialNumber, err)
			}
			continue
		}
		if err := ctrl.Send(serialNumber, messages.SetColor(&state.hue, &state.saturation, &state.brightness, &state.kelvin, transition, 0)); err != nil {
			return fmt.Errorf("set colour on %s: %w", serialNumber, err)
		}
		if err := ctrl.Send(serialNumber, messages.SetPowerOn(transition)); err != nil {
			return fmt.Errorf("turn on %s: %w", serialNumber, err)
		}
	}
	return nil
}

func wait(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func describeDevices(devices []device.Device) string {
	if len(devices) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(devices))
	for _, discovered := range devices {
		parts = append(parts, fmt.Sprintf("%q (%s)", discovered.Label, discovered.Serial))
	}
	return strings.Join(parts, ", ")
}

func describeDevice(serialNumber device.Serial, devices []device.Device) string {
	for _, discovered := range devices {
		if discovered.Serial == serialNumber {
			return fmt.Sprintf("%q (%s)", discovered.Label, serialNumber)
		}
	}
	return serialNumber.String()
}
