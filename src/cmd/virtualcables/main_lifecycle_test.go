package main

import (
	"context"
	"io"
	"log"
	"strings"
	"testing"
)

func TestRestartDefersUntilPreviousServerHasStopped(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	previousDone := make(chan error, 1)
	server := &appServer{
		cancel:  cancel,
		done:    previousDone,
		count:   1,
		address: "127.0.0.1:0",
		logger:  log.New(io.Discard, "", 0),
	}

	err := server.Restart(2)
	if err == nil || !strings.Contains(err.Error(), "shutdown did not complete") {
		t.Fatalf("restart should report unfinished shutdown, got %v", err)
	}
	if server.done != previousDone || server.cancel == nil || server.count != 1 {
		t.Fatal("restart discarded the previous server handle or changed cable count")
	}

	previousDone <- nil
	server.Stop()
	if server.done != nil || server.cancel != nil {
		t.Fatal("completed previous server was not reaped")
	}
}
