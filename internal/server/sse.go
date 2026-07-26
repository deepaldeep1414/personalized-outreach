package server

import "sync"

// SSEBroker manages per-run Server-Sent Events subscriptions.
// It stores a history of all published events so that a client which connects
// after a run has partially (or fully) completed receives a full replay before
// any live events.
type SSEBroker struct {
	mu          sync.Mutex
	history     map[string][][]byte     // runID → ordered published payloads
	subscribers map[string][]chan []byte // runID → active subscriber channels
	done        map[string]bool         // runID → run finished
}

func newSSEBroker() *SSEBroker {
	return &SSEBroker{
		history:     make(map[string][][]byte),
		subscribers: make(map[string][]chan []byte),
		done:        make(map[string]bool),
	}
}

// Publish stores data in history and fans it out to all current subscribers.
func (b *SSEBroker) Publish(runID string, data []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.history[runID] = append(b.history[runID], data)
	for _, ch := range b.subscribers[runID] {
		select {
		case ch <- data:
		default: // slow consumer; they'll catch up via history on reconnect
		}
	}
}

// Finish publishes a final event, marks the run closed, and closes every
// subscriber channel so ranging goroutines exit cleanly.
func (b *SSEBroker) Finish(runID string, finalData []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if finalData != nil {
		b.history[runID] = append(b.history[runID], finalData)
	}
	b.done[runID] = true
	for _, ch := range b.subscribers[runID] {
		if finalData != nil {
			select {
			case ch <- finalData:
			default:
			}
		}
		close(ch)
	}
	b.subscribers[runID] = nil
}

// Subscribe returns a snapshot of historical events and a live channel.
// The Subscribe + history-copy is atomic (locked), so no events are missed
// between "replay history" and "range live channel".
// If the run is already finished, ch is nil and alreadyDone is true.
func (b *SSEBroker) Subscribe(runID string) (history [][]byte, ch chan []byte, alreadyDone bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	hist := make([][]byte, len(b.history[runID]))
	copy(hist, b.history[runID])

	if b.done[runID] {
		return hist, nil, true
	}

	ch = make(chan []byte, 64)
	b.subscribers[runID] = append(b.subscribers[runID], ch)
	return hist, ch, false
}

// Unsubscribe removes a subscriber channel when the client disconnects.
func (b *SSEBroker) Unsubscribe(runID string, ch chan []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	subs := b.subscribers[runID]
	for i, s := range subs {
		if s == ch {
			b.subscribers[runID] = append(subs[:i], subs[i+1:]...)
			return
		}
	}
}
