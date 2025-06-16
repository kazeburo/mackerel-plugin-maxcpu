package statworker

import (
	"testing"
)

func TestCalculatingGap_FirstCallInitializesUsage(t *testing.T) {
	w := New()
	cpu := &cpuStat{
		User:      10,
		Nice:      20,
		System:    30,
		Idle:      40,
		Iowait:    50,
		IRQ:       60,
		SoftIRQ:   70,
		Steal:     80,
		Guest:     90,
		GuestNice: 100,
	}
	w.calculatingGap(cpu)
	if w.usages[0] == nil {
		t.Fatal("Expected usages[0] to be initialized")
	}
	got := w.usages[0]
	if got.User != 10 || got.Nice != 20 || got.System != 30 || got.Idle != 40 {
		t.Errorf("Unexpected values in usages[0]: %+v", got)
	}
}

func TestCalculatingGap_SecondCallComputesGapAndUsage(t *testing.T) {
	w := New()
	first := &cpuStat{
		User:      10,
		Nice:      20,
		System:    30,
		Idle:      40,
		Iowait:    50,
		IRQ:       60,
		SoftIRQ:   70,
		Steal:     80,
		Guest:     90,
		GuestNice: 100,
	}
	second := &cpuStat{
		User:      20,
		Nice:      25,
		System:    35,
		Idle:      50,
		Iowait:    55,
		IRQ:       65,
		SoftIRQ:   75,
		Steal:     85,
		Guest:     95,
		GuestNice: 105,
	}
	w.calculatingGap(first)
	w.calculatingGap(second)
	next := int64(1)
	got := w.usages[next]
	if got == nil {
		t.Fatalf("Expected usages[1] to be initialized")
	}
	if got.GapUser != 10 || got.GapNice != 5 || got.GapSystem != 5 || got.GapIdle != 10 {
		t.Errorf("Unexpected gap values: %+v", got)
	}
	// Usage calculation
	numerator := got.GapUser + got.GapSystem + got.GapIowait + got.GapSoftIRQ + got.GapSteal
	denominator := got.GapUser + got.GapNice + got.GapSystem + got.GapIdle + got.GapIowait + got.GapIRQ + got.GapSoftIRQ + got.GapSteal + got.GapGuest + got.GapGuestNice
	expectedUsage := (numerator / denominator) * 100.0
	if got.Usage != expectedUsage {
		t.Errorf("Expected Usage=%v, got %v", expectedUsage, got.Usage)
	}
}

func TestCalculatingGap_RingBufferWrapsAround(t *testing.T) {
	w := New()
	// Fill usages[0]
	w.calculatingGap(&cpuStat{})
	// Fill usages[1..historySize-1]
	for i := 1; i < historySize; i++ {
		w.calculatingGap(&cpuStat{User: float64(i)})
	}
	// Next call should wrap to usages[1]
	w.calculatingGap(&cpuStat{User: 999})
	if w.usages[1] == nil || w.usages[1].User != 999 {
		t.Errorf("Expected usages[1] to be overwritten with User=999, got %+v", w.usages[1])
	}
}
