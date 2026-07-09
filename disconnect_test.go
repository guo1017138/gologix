package gologix

import "testing"

func TestStartDisconnectAllowsConnectingCleanup(t *testing.T) {
	client := &Client{connStatus: connectionStatusConnecting}

	if err := client.startDisconnect(); err != nil {
		t.Fatalf("startDisconnect() returned error: %v", err)
	}

	if client.connStatus != connectionStatusDisconnecting {
		t.Fatalf("connStatus = %v, want %v", client.connStatus, connectionStatusDisconnecting)
	}
}
