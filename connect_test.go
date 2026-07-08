package gologix

import (
	"sync/atomic"
	"testing"
)

func TestForwardOpenConnectionSerialNumbersAreGlobal(t *testing.T) {
	oldSerial := atomic.LoadUint32(&connectionSerialValue)
	t.Cleanup(func() {
		atomic.StoreUint32(&connectionSerialValue, oldSerial)
	})
	atomic.StoreUint32(&connectionSerialValue, 41)

	client1 := NewClient("192.0.2.1")
	if _, err := client1.newForwardOpenLarge(); err != nil {
		t.Fatalf("newForwardOpenLarge client1 returned error: %v", err)
	}

	client2 := NewClient("192.0.2.1")
	if _, err := client2.newForwardOpenLarge(); err != nil {
		t.Fatalf("newForwardOpenLarge client2 returned error: %v", err)
	}

	if client1.ConnectionSerialNumber == client2.ConnectionSerialNumber {
		t.Fatalf("ConnectionSerialNumber should be unique across clients, got %d", client1.ConnectionSerialNumber)
	}
	if client1.ConnectionSerialNumber != 42 {
		t.Fatalf("client1 ConnectionSerialNumber = %d, want 42", client1.ConnectionSerialNumber)
	}
	if client2.ConnectionSerialNumber != 43 {
		t.Fatalf("client2 ConnectionSerialNumber = %d, want 43", client2.ConnectionSerialNumber)
	}
}
