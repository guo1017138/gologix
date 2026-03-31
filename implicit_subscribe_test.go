package gologix

import (
	"bytes"
	"testing"
)

func TestBuildImplicitAssemblyPath(t *testing.T) {
	path, err := BuildImplicitAssemblyPath(ImplicitAssemblyPathConfig{
		ConfigInstance: 151,
		OutputPoint:    150,
		InputPoint:     100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []byte{0x20, 0x04, 0x24, 0x97, 0x2C, 0x96, 0x2C, 0x64}
	if !bytes.Equal(path, want) {
		t.Fatalf("path mismatch\nwant=%v\n got=%v", want, path)
	}
}

func TestBuildImplicitAssemblyPathRequiresPoints(t *testing.T) {
	_, err := BuildImplicitAssemblyPath(ImplicitAssemblyPathConfig{})
	if err == nil {
		t.Fatalf("expected error for missing input/output points")
	}
}

func TestNewABGenericEthernetModuleAssemblyPath(t *testing.T) {
	cfg := NewABGenericEthernetModuleAssemblyPath(150, 100, 151)
	if cfg.AssemblyClass != CipObject_Assembly {
		t.Fatalf("expected assembly class %d, got %d", CipObject_Assembly, cfg.AssemblyClass)
	}
	if cfg.OutputPoint != CIPConnectionPoint(150) {
		t.Fatalf("unexpected output point: %d", cfg.OutputPoint)
	}
	if cfg.InputPoint != CIPConnectionPoint(100) {
		t.Fatalf("unexpected input point: %d", cfg.InputPoint)
	}
	if cfg.ConfigInstance != CIPInstance(151) {
		t.Fatalf("unexpected config instance: %d", cfg.ConfigInstance)
	}
}

func TestNewABGenericEthernetModuleAssemblyPathPtr(t *testing.T) {
	ptr := NewABGenericEthernetModuleAssemblyPathPtr(150, 100, 0)
	if ptr == nil {
		t.Fatalf("expected non-nil pointer")
	}
	if ptr.ConfigInstance != 0 {
		t.Fatalf("expected omitted config instance")
	}
}

func TestNewABGenericEthernetModuleAssemblyPathDefault(t *testing.T) {
	ptr := NewABGenericEthernetModuleAssemblyPathDefault()
	if ptr == nil {
		t.Fatalf("expected non-nil pointer")
	}
	if ptr.OutputPoint != CIPConnectionPoint(ABGenericEthernetModuleOutputInstanceDefault) {
		t.Fatalf("unexpected default output point: %d", ptr.OutputPoint)
	}
	if ptr.InputPoint != CIPConnectionPoint(ABGenericEthernetModuleInputInstanceDefault) {
		t.Fatalf("unexpected default input point: %d", ptr.InputPoint)
	}
	if ptr.ConfigInstance != CIPInstance(ABGenericEthernetModuleConfigInstanceDefault) {
		t.Fatalf("unexpected default config instance: %d", ptr.ConfigInstance)
	}
}

func TestBuildImplicitProducedConsumedPath(t *testing.T) {
	path, err := BuildImplicitProducedConsumedPath(ImplicitProducedConsumedPathConfig{
		ConsumedTag:     "MyConsumedTag",
		IncludeKeying:   true,
		KeyingWords:     4,
		IOClass:         CipObject_IOClass,
		IOInstance:      1,
		ConnectionPoint: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(path) < 16 {
		t.Fatalf("path too short: %d", len(path))
	}
	if !bytes.Equal(path[:8], []byte{0, 0, 0, 0, 0, 0, 0, 0}) {
		t.Fatalf("expected 4 keying words prefix, got %v", path[:8])
	}
	if path[8] != 0x20 || path[9] != 0x69 {
		t.Fatalf("expected IO class segment 20 69, got %02X %02X", path[8], path[9])
	}
	if path[10] != 0x24 || path[11] != 0x01 {
		t.Fatalf("expected instance segment 24 01, got %02X %02X", path[10], path[11])
	}
	if path[len(path)-2] != 0x2C || path[len(path)-1] != 0x01 {
		t.Fatalf("expected connection point segment 2C 01 at end, got ... %02X %02X", path[len(path)-2], path[len(path)-1])
	}
}

func TestBuildImplicitProducedConsumedPathRequiresTag(t *testing.T) {
	_, err := BuildImplicitProducedConsumedPath(ImplicitProducedConsumedPathConfig{})
	if err == nil {
		t.Fatalf("expected error for empty consumed tag")
	}
}

func TestNewProducedConsumedPathDefault(t *testing.T) {
	cfg := NewProducedConsumedPathDefault("Program:Main.ConsumedTag")
	if cfg == nil {
		t.Fatalf("expected non-nil config")
	}
	if cfg.ConsumedTag == "" {
		t.Fatalf("expected consumed tag in default config")
	}
	if cfg.IOClass != CipObject_IOClass {
		t.Fatalf("unexpected IO class %d", cfg.IOClass)
	}
	if cfg.IOInstance != 1 || cfg.ConnectionPoint != 1 {
		t.Fatalf("unexpected defaults: instance=%d cp=%d", cfg.IOInstance, cfg.ConnectionPoint)
	}
}
