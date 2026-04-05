package sse

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
)

// Client represents a single SSE connection.
type Client struct {
	send chan []byte
	done chan struct{}
}

// Broker manages SSE client connections and broadcasts events to all of them.
type Broker struct {
	mu      sync.RWMutex
	clients map[*Client]struct{}
}

// NewBroker creates a new SSE broker.
func NewBroker() *Broker {
	return &Broker{
		clients: make(map[*Client]struct{}),
	}
}

// addClient registers a new SSE client.
func (b *Broker) addClient() *Client {
	c := &Client{
		send: make(chan []byte, 64),
		done: make(chan struct{}),
	}
	b.mu.Lock()
	b.clients[c] = struct{}{}
	b.mu.Unlock()
	slog.Debug("sse: client connected", "total", b.clientCount())
	return c
}

// removeClient unregisters an SSE client.
func (b *Broker) removeClient(c *Client) {
	b.mu.Lock()
	delete(b.clients, c)
	b.mu.Unlock()
	close(c.done)
	slog.Debug("sse: client disconnected", "total", b.clientCount())
}

func (b *Broker) clientCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients)
}

// Broadcast sends an SSE message to every connected client.
// eventType is the SSE event name; data is the raw JSON payload.
func (b *Broker) Broadcast(eventType string, data []byte) {
	msg := fmt.Appendf(nil, "event: %s\ndata: %s\n\n", eventType, data)

	b.mu.RLock()
	defer b.mu.RUnlock()

	for c := range b.clients {
		select {
		case c.send <- msg:
		default:
			slog.Warn("sse: client send buffer full, dropping event",
				"event", eventType)
		}
	}
}

// BroadcastJSON marshals payload to JSON and broadcasts it.
func (b *Broker) BroadcastJSON(eventType string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		slog.Error("sse: failed to marshal event payload",
			"event", eventType, "error", err)
		return
	}
	b.Broadcast(eventType, data)
}

// ServeHTTP implements http.Handler for the SSE endpoint.
func (b *Broker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	client := b.addClient()
	defer b.removeClient(client)

	// Send initial comment to confirm connection.
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-client.done:
			return
		case msg := <-client.send:
			_, err := w.Write(msg)
			if err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
