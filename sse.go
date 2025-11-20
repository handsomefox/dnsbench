package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// SSEEvent represents a server-sent event message pushed to UI clients.
type SSEEvent struct {
	RunID  string      `json:"runId"`
	Type   string      `json:"type"`
	Detail interface{} `json:"detail,omitempty"`
}

type sseClient struct {
	id   int
	ch   chan SSEEvent
	done chan struct{}
}

// SSEHub manages SSE clients.
type SSEHub struct {
	mu      sync.RWMutex
	clients map[int]*sseClient
	nextID  int
}

func NewSSEHub() *SSEHub {
	return &SSEHub{
		clients: make(map[int]*sseClient),
	}
}

// Add registers a new SSE client.
func (h *SSEHub) Add() (client *sseClient, ch <-chan SSEEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.nextID++
	c := &sseClient{
		id:   h.nextID,
		ch:   make(chan SSEEvent, 32),
		done: make(chan struct{}),
	}
	h.clients[c.id] = c
	return c, c.ch
}

// Remove unregisters a client.
func (h *SSEHub) Remove(id int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c, ok := h.clients[id]; ok {
		close(c.done)
		close(c.ch)
		delete(h.clients, id)
	}
}

// Broadcast sends an event to all clients.
func (h *SSEHub) Broadcast(evt SSEEvent) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, c := range h.clients {
		select {
		case c.ch <- evt:
		default:
			slog.Warn(
				"dropping SSE event for slow client",
				slog.Int("client_id", c.id),
				slog.String("type", evt.Type),
			)
		}
	}
}

// Handle attaches an SSE stream to the response writer.
func (h *SSEHub) Handle(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	client, ch := h.Add()
	defer h.Remove(client.id)

	// Initial ready event for clients.
	writeEvent(w, flusher, SSEEvent{Type: "ready"})

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				return
			}
			writeEvent(w, flusher, evt)
		case <-heartbeat.C:
			writeEvent(w, flusher, SSEEvent{Type: "ping"})
		case <-r.Context().Done():
			return
		}
	}
}

func writeEvent(w http.ResponseWriter, flusher http.Flusher, evt SSEEvent) {
	b, err := json.Marshal(evt)
	if err != nil {
		slog.Error("failed to encode SSE event", slogErr(err))
		return
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
		slog.Warn("failed to write SSE event", slogErr(err))
		return
	}
	flusher.Flush()
}
