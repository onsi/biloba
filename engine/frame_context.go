package engine

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/chromedp/cdproto"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

type frameWorld struct {
	id       runtime.ExecutionContextID
	uniqueID string
}

type frameWorldsKey struct{}

type frameWorlds struct {
	mu      sync.RWMutex
	byFrame map[cdp.FrameID]frameWorld
}

// frameWorldsTimeout bounds the two Runtime commands trackFrameWorlds sends, which a healthy Chrome
// answers in milliseconds; it is a backstop, like the Go client's cdpTimeout.
const frameWorldsTimeout = 30 * time.Second

/*
trackFrameWorlds starts the frame-world registry of an attached target: the page JavaScript context of
each frame document, as the target announces them.  This registry belongs to the renderer attachment,
not to any one frame handle.

Chrome announces the contexts that already exist only when Runtime is enabled, and chromedp enabled it
when it attached, before anything could listen.  Enabling it again announces nothing, so this
disables it, starts listening, and enables it again: Chrome then announces every context the target
has, and while Runtime is disabled no context event arrives, so the registry starts from exactly what
exists.  The two round trips are the price of each tracked attachment; the enable also replays the
console messages Chrome kept, which nothing listens to yet at attach time.
*/
//
// bound, when not nil, also ends the two commands: the caller's own deadline for bringing the target up.
func trackFrameWorlds(ctx, bound context.Context) (context.Context, error) {
	callCtx, cancel := context.WithTimeout(ctx, frameWorldsTimeout)
	defer cancel()
	if bound != nil {
		defer context.AfterFunc(bound, cancel)()
	}
	if _, err := chromedp.Call(callCtx, runtime.Disable, cdp.Empty{}); err != nil {
		return nil, err
	}
	worlds := &frameWorlds{byFrame: map[cdp.FrameID]frameWorld{}}
	err := ListenTarget(ctx, func(event any) {
		worlds.mu.Lock()
		defer worlds.mu.Unlock()
		switch event := event.(type) {
		case *runtime.EventExecutionContextCreated:
			var aux struct {
				FrameID   cdp.FrameID `json:"frameId"`
				IsDefault bool        `json:"isDefault"`
			}
			if json.Unmarshal(event.Context.AuxData, &aux) == nil && aux.IsDefault && aux.FrameID != "" {
				worlds.byFrame[aux.FrameID] = frameWorld{id: event.Context.ID, uniqueID: event.Context.UniqueID}
			}
		case *runtime.EventExecutionContextDestroyed:
			for id, world := range worlds.byFrame {
				if world.uniqueID == event.ExecutionContextUniqueID {
					delete(worlds.byFrame, id)
				}
			}
		case *runtime.EventExecutionContextsCleared:
			clear(worlds.byFrame)
		}
	}, runtime.ExecutionContextCreated.Method, runtime.ExecutionContextDestroyed.Method, runtime.ExecutionContextsCleared.Method)
	if err != nil {
		return nil, err
	}
	if _, err := chromedp.Call(callCtx, runtime.Enable, cdp.Empty{}); err != nil {
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
