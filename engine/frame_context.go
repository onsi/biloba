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

A listener on the session's target keeps it, so the session's other listeners see it as of the event
they are handed.  connection.worldsFor registers that listener and seeds the registry with the
contexts the target announced while chromedp attached it, so nothing asks Chrome to announce them
again.
*/
type frameWorlds struct {
	mu      sync.RWMutex
	byFrame map[cdp.FrameID]frameWorld
}

func newFrameWorlds() *frameWorlds {
	return &frameWorlds{byFrame: map[cdp.FrameID]frameWorld{}}
}

// frameWorldEvent reports whether a method changes the frame-world registry.
func frameWorldEvent(method string) bool {
	switch method {
	case runtime.ExecutionContextCreated.Method, runtime.ExecutionContextDestroyed.Method, runtime.ExecutionContextsCleared.Method:
		return true
	}
	return false
}

// apply is the registry's listener.
func (w *frameWorlds) apply(ev any) {
	switch ev.(type) {
	case *runtime.EventExecutionContextCreated, *runtime.EventExecutionContextDestroyed, *runtime.EventExecutionContextsCleared:
		w.mu.Lock()
		defer w.mu.Unlock()
		applyFrameWorldEvent(w.byFrame, ev)
	}
}

// applyFrameWorldEvent applies one context event to a registry's frames.
func applyFrameWorldEvent(byFrame map[cdp.FrameID]frameWorld, ev any) {
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
			byFrame[aux.FrameID] = frameWorld{id: ev.Context.ID, uniqueID: ev.Context.UniqueID}
		}
	case *runtime.EventExecutionContextDestroyed:
		for id, world := range byFrame {
			if world.uniqueID == ev.ExecutionContextUniqueID {
				delete(byFrame, id)
			}
		}
	case *runtime.EventExecutionContextsCleared:
		clear(byFrame)
	}
}

// trackFrameWorlds gives an attached target's context its session's frame-world registry (see
// connection.worldsFor).  It sends no command.  Call it after the first Run on ctx, on the goroutine
// that ran it or after it, since that Run sets the context's target.
func trackFrameWorlds(ctx context.Context) (context.Context, error) {
	conn, c, err := connectionFor(ctx)
	if err != nil {
		return nil, err
	}
	if c.Target == nil {
		return nil, errors.New("tracking frame worlds needs an attached target: run the context once first")
	}
	worlds, err := conn.worldsFor(ctx, c.Target.SessionID)
	if err != nil {
		return nil, err
	}
	return context.WithValue(ctx, frameWorldsKey{}, worlds), nil
}

func mainFrameWorld(ctx context.Context, id cdp.FrameID) (frameWorld, error) {
	worlds, _ := ctx.Value(frameWorldsKey{}).(*frameWorlds)
	if worlds != nil {
		worlds.mu.RLock()
		world, found := worlds.byFrame[id]
		worlds.mu.RUnlock()
		if found && world.id != 0 && world.uniqueID != "" {
			return world, nil
		}
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
