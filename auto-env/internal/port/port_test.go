package port

import (
	"testing"
)

func TestAllocateBasic(t *testing.T) {
	names := []string{"caddy", "db", "firestore", "web"}
	ports, err := Allocate(names, 3000, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"caddy": 3000, "db": 3001, "firestore": 3002, "web": 3003}
	for k, v := range want {
		if ports[k] != v {
			t.Errorf("Port[%s] = %d, want %d", k, ports[k], v)
		}
	}
}

func TestAllocateSlotOffset(t *testing.T) {
	names := []string{"caddy", "db", "firestore", "web"}
	ports, err := Allocate(names, 3000, 100, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"caddy": 3200, "db": 3201, "firestore": 3202, "web": 3203}
	for k, v := range want {
		if ports[k] != v {
			t.Errorf("Port[%s] = %d, want %d", k, ports[k], v)
		}
	}
}

func TestAllocateExceedsStride(t *testing.T) {
	names := make([]string, 101)
	for i := range names {
		names[i] = "port"
	}
	_, err := Allocate(names, 3000, 100, 0)
	if err == nil {
		t.Fatal("expected error when names exceed stride")
	}
}

func TestAllocateDeterministic(t *testing.T) {
	names := []string{"api", "web", "worker"}
	for i := range 5 {
		ports, err := Allocate(names, 3000, 100, 1)
		if err != nil {
			t.Fatal(err)
		}
		if ports["api"] != 3100 || ports["web"] != 3101 || ports["worker"] != 3102 {
			t.Fatalf("non-deterministic result on iteration %d: %v", i, ports)
		}
	}
}
