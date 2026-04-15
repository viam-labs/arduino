package arduino

import (
	"context"
	"testing"
)

func TestDigitalInterrupt_Name(t *testing.T) {
	di := &digitalInterrupt{name: "enc-a"}
	if di.Name() != "enc-a" {
		t.Fatalf("want enc-a got %s", di.Name())
	}
}

func TestDigitalInterrupt_Value_StartsZero(t *testing.T) {
	di := &digitalInterrupt{name: "btn"}
	v, err := di.Value(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if v != 0 {
		t.Fatalf("want 0 got %d", v)
	}
}

func TestDigitalInterrupt_Value_CountsTicks(t *testing.T) {
	di := &digitalInterrupt{name: "enc-a"}
	di.recordTick()
	di.recordTick()
	v, _ := di.Value(context.Background(), nil)
	if v != 2 {
		t.Fatalf("want 2 got %d", v)
	}
}
