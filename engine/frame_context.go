package engine

import (
	"context"
	"errors"
	"sync"

	"github.com/chromedp/cdproto"
	"github.com/chromedp/cdproto/cdp"
	jsonv2 "github.com/chromedp/cdproto/cdp/jsonv2"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

type frameWorld struct {
	id       runtime.ExecutionContextID
	uniqueID string
}

type frameWorldsKey struct{}

/*
frameWorlds is the frame-world registry of one session: the page JavaScript context of each frame
document, as the session announces them.  It belongs to the renderer attachment, not to any one frame
handle.

newTabContext makes one for each chromedp context it makes, gives chromedp its apply as the context's
first listener (chromedp.WithListener), and puts it on the context.  chromedp registers apply before
it sends the target its first command, so the registry sees the contexts the target already had,
which Chrome announces in reply to chromedp's Runtime.enable, and nothing asks Chrome to announce them
again.  Being the first listener, it is up to date with the event each of the session's other
listeners is handed.  It lives as long as the context that holds it.
*/
type frameWorlds struct {
	mu      sync.RWMutex
	byFrame map[cdp.FrameID]frameWorld
}

func newFrameWorlds() *frameWorlds {
	return &frameWorlds{byFrame: map[cdp.FrameID]frameWorld{}}
}

// apply is the registry's listener.
func (w *frameWorlds) apply(ev any) {
	switch ev := ev.(type) {
	case *runtime.EventExecutionContextCreated:
		if ev.Context == nil {
			return
		}
		var aux struct {
			FrameID   cdp.FrameID `json:"frameId"`
			IsDefault bool        `json:"isDefault"`
		}
		if jsonv2.Unmarshal(ev.Context.AuxData, &aux, chromedp.DefaultUnmarshalOptions) != nil {
			return
		}
		if aux.IsDefault && aux.FrameID != "" {
			w.mu.Lock()
			defer w.mu.Unlock()
			w.byFrame[aux.FrameID] = frameWorld{id: ev.Context.ID, uniqueID: ev.Context.UniqueID}
		}
	case *runtime.EventExecutionContextDestroyed:
		w.mu.Lock()
		defer w.mu.Unlock()
		for id, world := range w.byFrame {
			if world.uniqueID == ev.ExecutionContextUniqueID {
				delete(w.byFrame, id)
			}
		}
	case *runtime.EventExecutionContextsCleared:
		w.mu.Lock()
		defer w.mu.Unlock()
		clear(w.byFrame)
	}
}

func mainFrameWorld(ctx context.Context, id cdp.FrameID) (frameWorld, error) {
	worlds, _ := ctx.Value(frameWorldsKey{}).(*frameWorlds)
	if worlds == nil {
		return frameWorld{}, &Error{Code: CodeInvalidArgument, Operation: "attach frame", Message: "the context has no frame-world registry: its target was not attached through NewTabContext"}
	}
	worlds.mu.RLock()
	world, found := worlds.byFrame[id]
	worlds.mu.RUnlock()
	if found && world.id != 0 && world.uniqueID != "" {
		return world, nil
	}
	// Creation/navigation can briefly expose a frame before its context exists.
	// Never fall back to the target's default context, which could be the parent.
	return frameWorld{}, &Error{Code: CodeConditionNotMet, Operation: "attach frame", Message: "frame's page execution context is not available yet"}
}

// frameWorldCurrent compares document identity using the registry maintained by CDP events.
// It does not send a command to a potentially busy renderer.
func frameWorldCurrent(ctx context.Context, id cdp.FrameID, expected frameWorld) bool {
	world, err := mainFrameWorld(ctx, id)
	return ctx.Err() == nil && expected.uniqueID != "" && err == nil && world.uniqueID == expected.uniqueID
}

// Chrome can reject evaluation before it announces context destruction. Restrict
// this classification to protocol errors; page-thrown exceptions keep their meaning.
func frameContextGone(err error) bool {
	if errors.Is(err, chromedp.ErrTargetDetached) {
		return true // chromedp's for a command Chrome never answered: the target went away under it
	}
	var protocolErr *cdproto.Error
	if !errors.As(err, &protocolErr) {
		return false
	}
	switch {
	case protocolErr.Code == -32000:
		switch protocolErr.Message {
		case "Inspected target navigated or closed", "Cannot find context with specified id", "Execution context was destroyed.":
			return true
		}
	case protocolErr.Code == -32602:
		// An evaluation pinned by uniqueContextId to a document that has since been replaced.
		return protocolErr.Message == "uniqueContextId not found"
	}
	return false
}
