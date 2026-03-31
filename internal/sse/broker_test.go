package sse

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBrokerBroadcast(t *testing.T) {
	broker := NewBroker()

	srv := httptest.NewServer(broker)
	defer srv.Close()

	// Connect SSE client
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %s", resp.Header.Get("Content-Type"))
	}

	scanner := bufio.NewScanner(resp.Body)

	// Read initial comment
	readLine := func(timeout time.Duration) string {
		done := make(chan string, 1)
		go func() {
			if scanner.Scan() {
				done <- scanner.Text()
			}
		}()
		select {
		case line := <-done:
			return line
		case <-time.After(timeout):
			t.Fatal("timed out reading SSE line")
			return ""
		}
	}

	line := readLine(2 * time.Second)
	if !strings.HasPrefix(line, ": connected") {
		t.Errorf("expected connection comment, got: %s", line)
	}

	// Skip blank line after comment
	readLine(2 * time.Second)

	// Broadcast an event
	broker.Broadcast("test.event", []byte(`{"foo":"bar"}`))

	// Read event line
	eventLine := readLine(2 * time.Second)
	if eventLine != "event: test.event" {
		t.Errorf("expected 'event: test.event', got: %s", eventLine)
	}

	dataLine := readLine(2 * time.Second)
	if dataLine != `data: {"foo":"bar"}` {
		t.Errorf("expected data line, got: %s", dataLine)
	}
}

func TestBrokerMultipleClients(t *testing.T) {
	broker := NewBroker()

	srv := httptest.NewServer(broker)
	defer srv.Close()

	// Connect two clients
	resp1, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp1.Body.Close()

	resp2, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()

	// Wait for both clients to be registered
	time.Sleep(50 * time.Millisecond)

	if broker.clientCount() != 2 {
		t.Errorf("expected 2 clients, got %d", broker.clientCount())
	}
}
