package engine

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// DefaultDebugQueueSize bounds pending entries ahead of a debug sink.
	DefaultDebugQueueSize = 256
	// DefaultDebugEntryBytes bounds the retained text for one CDP debug entry.
	DefaultDebugEntryBytes = 16 << 10
)

// DebugDirection identifies whether Chrome sent or received a CDP message.
type DebugDirection string

const (
	DebugSend     DebugDirection = "send"
	DebugReceive  DebugDirection = "receive"
	DebugInternal DebugDirection = "internal"
)

// DebugEntry is one bounded structured CDP debug record.
type DebugEntry struct {
	Timestamp time.Time
	Direction DebugDirection
	Message   string
	Truncated bool
}

type debugDispatcher struct {
	sink    func(DebugEntry)
	queue   chan DebugEntry
	stopped chan struct{}
	dropped atomic.Uint64
	once    sync.Once
}

func newDebugDispatcher(sink func(DebugEntry)) *debugDispatcher {
	if sink == nil {
		return nil
	}
	dispatcher := &debugDispatcher{
		sink: sink, queue: make(chan DebugEntry, DefaultDebugQueueSize), stopped: make(chan struct{}),
	}
	go dispatcher.run()
	return dispatcher
}

func (d *debugDispatcher) run() {
	for {
		select {
		case <-d.stopped:
			return
		case entry := <-d.queue:
			func() {
				defer func() { _ = recover() }()
				d.sink(entry)
			}()
		}
	}
}

func (d *debugDispatcher) logf(format string, args ...any) {
	if d == nil {
		return
	}
	message := fmt.Sprintf(format, args...)
	direction := DebugInternal
	if strings.HasPrefix(message, "-> ") {
		direction, message = DebugSend, strings.TrimPrefix(message, "-> ")
	} else if strings.HasPrefix(message, "<- ") {
		direction, message = DebugReceive, strings.TrimPrefix(message, "<- ")
	}
	truncated := len(message) > DefaultDebugEntryBytes
	if truncated {
		message = truncateUTF8(message, DefaultDebugEntryBytes)
	}
	entry := DebugEntry{Timestamp: time.Now(), Direction: direction, Message: message, Truncated: truncated}
	select {
	case <-d.stopped:
		return
	default:
	}
	select {
	case d.queue <- entry:
	default:
		d.dropped.Add(1)
	}
}

func (d *debugDispatcher) stop() {
	if d != nil {
		d.once.Do(func() { close(d.stopped) })
	}
}

func (d *debugDispatcher) droppedCount() uint64 {
	if d == nil {
		return 0
	}
	return d.dropped.Load()
}
