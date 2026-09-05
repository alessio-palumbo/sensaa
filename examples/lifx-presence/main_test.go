package main

import (
	"math"
	"testing"

	"github.com/alessio-palumbo/sensaa"
)

func TestDistanceBrightness(t *testing.T) {
	for _, test := range []struct{ distance, want float64 }{
		{0.25, 100}, {0.5, 100}, {1, 85}, {1.5, 70}, {2.25, 50}, {3, 30}, {4, 30},
	} {
		if got := distanceBrightness(test.distance); math.Abs(got-test.want) > 0.001 {
			t.Errorf("distanceBrightness(%v) = %v, want %v", test.distance, got, test.want)
		}
	}
}

func TestStateForUpdate(t *testing.T) {
	off, err := stateForUpdate("count", false, 30, sensaa.Update{})
	if err != nil || off.poweredOn {
		t.Fatalf("off state = %+v, %v", off, err)
	}

	update := sensaa.Update{Presence: true, Targets: []sensaa.Target{
		{PositionMM: sensaa.PositionMM{X: 3000, Y: 4000}},
		{PositionMM: sensaa.PositionMM{X: 300, Y: 400}},
	}}
	distance, err := stateForUpdate("distance", false, 30, update)
	if err != nil {
		t.Fatal(err)
	}
	if distance.distanceM != 0.5 || distance.brightness != 100 {
		t.Fatalf("distance state = %+v", distance)
	}
	count, err := stateForUpdate("count", false, 30, update)
	if err != nil {
		t.Fatal(err)
	}
	if count.name != "blue" || count.targets != 2 {
		t.Fatalf("count state = %+v", count)
	}
}

func TestStabilizer(t *testing.T) {
	s := stabilizer{required: 3}
	if s.observe("one") || s.observe("one") || !s.observe("one") {
		t.Fatal("state did not stabilize on its third frame")
	}
	s.commit("one")
	if s.observe("one") {
		t.Fatal("committed state triggered again")
	}
}
