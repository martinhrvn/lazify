package ui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/martinhrvn/lazify/internal/engine"
)

// streamEvent is one message from a running stream's goroutine.
type streamEvent struct {
	data []byte
	done bool
	err  error
}

// streamMsg delivers everything a stream produced since the last one.
type streamMsg struct {
	id   uint64
	data []byte
	done bool
	err  error
}

// startStream runs r in the background and returns the command that waits for
// its first output. Sends give up once ctx is cancelled, so a killed stream's
// goroutine never blocks.
func (m Model) startStream(ctx context.Context, r engine.Run) tea.Cmd {
	ch := make(chan streamEvent, 64)
	m.streams[r.ID] = ch // Update re-issues waitStream while the id is here
	send := func(ev streamEvent) {
		select {
		case ch <- ev:
		case <-ctx.Done():
		}
	}
	go func() {
		defer close(ch)
		err := m.runner.Stream(ctx, r.Req, func(b []byte) { send(streamEvent{data: b}) })
		send(streamEvent{done: true, err: err})
	}()
	return waitStream(r.ID, ch)
}

// waitStream blocks for the next stream event, then gathers whatever else is
// already buffered so a chatty stream re-renders once per batch, not per chunk.
func waitStream(id uint64, ch <-chan streamEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		msg := streamMsg{id: id, data: ev.data, done: ev.done, err: ev.err}
		for !msg.done {
			select {
			case ev, ok := <-ch:
				if !ok {
					return msg
				}
				msg.data = append(msg.data, ev.data...)
				msg.done, msg.err = ev.done, ev.err
			default:
				return msg
			}
		}
		return msg
	}
}
