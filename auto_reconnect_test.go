package gologix

import (
	"errors"
	"testing"
)

func TestCheckConnectionAutoReconnectInProgress(t *testing.T) {
	client := &Client{
		AutoConnect: true,
		connStatus:  connectionStatusConnecting,
	}

	err := client.checkConnection()
	if !errors.Is(err, ErrAutoReconnectInProgress) {
		t.Fatalf("checkConnection() error = %v, want ErrAutoReconnectInProgress", err)
	}
}

func TestReadMapSkipsDuringAutoReconnectInProgress(t *testing.T) {
	client := &Client{
		AutoConnect: true,
		connStatus:  connectionStatusConnecting,
	}

	processed, err := client.ReadMap(map[string]any{"TestTag": new(int32)})
	if err != nil {
		t.Fatalf("ReadMap() error = %v, want nil", err)
	}
	if len(processed) != 0 {
		t.Fatalf("ReadMap() processed %d tags, want 0", len(processed))
	}
}
