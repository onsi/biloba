package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/onsi/biloba/engine"
	"github.com/onsi/biloba/protocol"
)

func (s *engineSession) ExecuteEventful(ctx context.Context, operation protocol.EventfulOperation) (any, error) {
	switch operation.Kind {
	case protocol.EventfulRegisterDialogHandler:
		options := engine.DialogHandlerOptions{Type: engine.DialogType(operation.DialogType), Accept: operation.Accept, PromptText: operation.PromptText}
		if operation.Message != nil {
			value, err := expectationFromProtocol(*operation.Message)
			if err != nil {
				return nil, err
			}
			options.Message = &value
		}
		handler, err := s.session.RegisterDialogHandler(ctx, options)
		return map[string]any{"id": handler.ID}, engineRPCError(err)
	case protocol.EventfulRemoveDialogHandler:
		return nil, engineRPCError(s.session.RemoveDialogHandler(ctx, operation.ID))
	case protocol.EventfulDialogs:
		query, err := dialogQueryFromProtocol(operation)
		if err != nil {
			return nil, err
		}
		return dialogsToWire(s.session.DialogsMatching(query)), nil
	case protocol.EventfulWarnings:
		return warningsToWire(s.session.Warnings()), nil
	case protocol.EventfulDownloads:
		query, err := downloadQueryFromProtocol(operation)
		if err != nil {
			return nil, err
		}
		return downloadsToWire(s.session.Downloads(query)), nil
	case protocol.EventfulWaitForDownload:
		query, err := downloadQueryFromProtocol(operation)
		if err != nil {
			return nil, err
		}
		value, waitErr := s.session.WaitForDownload(ctx, query, pollPolicyFromProtocol(operation.Poll))
		return downloadToWire(value), engineRPCError(waitErr)
	case protocol.EventfulDownloadContent:
		body, err := s.session.DownloadContent(ctx, operation.ID, operation.MaxBodyBytes)
		if err != nil {
			return nil, engineRPCError(err)
		}
		return map[string]any{"bodyBase64": base64.StdEncoding.EncodeToString(body)}, nil
	case protocol.EventfulCancelDownload:
		return nil, engineRPCError(s.session.CancelDownload(ctx, operation.ID))
	case protocol.EventfulRequests:
		query, err := requestQueryFromProtocol(operation)
		if err != nil {
			return nil, err
		}
		return requestsToWire(s.session.RequestsMatching(query)), nil
	case protocol.EventfulWaitForRequest:
		query, err := requestQueryFromProtocol(operation)
		if err != nil {
			return nil, err
		}
		value, waitErr := s.session.WaitForRequest(ctx, query, pollPolicyFromProtocol(operation.Poll))
		return requestToWire(value), engineRPCError(waitErr)
	case protocol.EventfulResponses:
		query, err := responseQueryFromProtocol(operation)
		if err != nil {
			return nil, err
		}
		return responsesToWire(s.session.Responses(query)), nil
	case protocol.EventfulWaitForNetworkIdle:
		result, err := s.session.WaitForNetworkIdle(ctx, pollPolicyFromProtocol(operation.Poll))
		return eventfulAssertionResult(result), engineRPCError(err)
	case protocol.EventfulRegisterNetworkHandler:
		options, err := networkHandlerOptionsFromProtocol(operation)
		if err != nil {
			return nil, err
		}
		handler, registerErr := s.session.RegisterNetworkHandler(ctx, options)
		return map[string]any{"id": handler.ID}, engineRPCError(registerErr)
	case protocol.EventfulRemoveNetworkHandler:
		return nil, engineRPCError(s.session.RemoveNetworkHandler(ctx, operation.ID))
	case protocol.EventfulNetworkHandlerStats:
		value, err := s.session.NetworkHandlerStats(operation.ID)
		return map[string]any{"id": value.ID, "callsite": value.Callsite, "count": value.Count, "shadowed": value.Shadowed, "lastError": value.LastError}, engineRPCError(err)
	case protocol.EventfulNetworkShadows:
		return shadowsToWire(s.session.NetworkShadowDiagnostics()), nil
	case protocol.EventfulHoldResponse:
		if operation.URL == nil {
			return nil, protocol.NewError(protocol.CodeInvalidArgument, "url expectation is required")
		}
		expected, err := expectationFromProtocol(*operation.URL)
		if err != nil {
			return nil, err
		}
		id, holdErr := s.session.HoldResponseWithOptions(ctx, expected, engine.ResponseHoldOptions{Limit: operation.Limit, MaxBodyBytes: operation.MaxBodyBytes, Callsite: operation.Callsite})
		return map[string]any{"id": id}, engineRPCError(holdErr)
	case protocol.EventfulAwaitResponseHold:
		value, err := s.session.AwaitResponseHold(ctx, operation.ID)
		return heldResponseToWire(value), engineRPCError(err)
	case protocol.EventfulReleaseResponseHold:
		return nil, engineRPCError(s.session.ReleaseResponseHold(ctx, operation.ID))
	case protocol.EventfulReleaseHeldResponse:
		return nil, engineRPCError(s.session.ReleaseHeldResponse(ctx, operation.ID, operation.ResponseID))
	case protocol.EventfulReleaseNextResponse:
		return nil, engineRPCError(s.session.ReleaseNextResponseHold(ctx, operation.ID))
	case protocol.EventfulResponseHoldStats:
		value, err := s.session.ResponseHoldStats(operation.ID)
		return map[string]any{"count": value.Count, "held": value.Held, "passedThrough": value.PassedThrough, "holding": value.Holding, "lastError": value.LastError}, engineRPCError(err)
	case protocol.EventfulSetNetworkState:
		return nil, engineRPCError(s.session.SetNetworkState(ctx, networkStateFromWire(operation.Network)))
	case protocol.EventfulNetworkState:
		return networkStateToWire(s.session.CurrentNetworkState()), nil
	case protocol.EventfulSetCacheEnabled:
		return nil, engineRPCError(s.session.SetCacheEnabled(ctx, operation.CacheEnabled))
	default:
		return nil, protocol.NewError(protocol.CodeInvalidArgument, "unsupported eventful operation")
	}
}

func protocolExpectation(value *protocol.Expectation) (*engine.Expectation, error) {
	if value == nil {
		return nil, nil
	}
	converted, err := expectationFromProtocol(*value)
	return &converted, err
}
func dialogQueryFromProtocol(o protocol.EventfulOperation) (engine.DialogQuery, error) {
	message, err := protocolExpectation(o.Message)
	return engine.DialogQuery{Type: engine.DialogType(o.DialogType), Message: message}, err
}
func downloadQueryFromProtocol(o protocol.EventfulOperation) (engine.DownloadQuery, error) {
	filename, err := protocolExpectation(o.Filename)
	if err != nil {
		return engine.DownloadQuery{}, err
	}
	url, err := protocolExpectation(o.URL)
	if err != nil {
		return engine.DownloadQuery{}, err
	}
	state, err := protocolExpectation(o.State)
	if err != nil {
		return engine.DownloadQuery{}, err
	}
	query := engine.DownloadQuery{Filename: filename, URL: url}
	content, err := protocolExpectation(o.ContentText)
	if err != nil {
		return engine.DownloadQuery{}, err
	}
	query.Content = content
	if o.ContentBase64 != nil {
		body, decodeErr := decodeBoundedBody(*o.ContentBase64)
		if decodeErr != nil {
			return engine.DownloadQuery{}, decodeErr
		}
		query.ContentBytes = body
	}
	if state != nil && state.Kind == engine.ExpectEqual {
		if text, ok := state.Expected.(string); ok {
			query.State = engine.DownloadState(text)
		}
	}
	return query, nil
}
func requestQueryFromProtocol(o protocol.EventfulOperation) (engine.RequestQuery, error) {
	url, err := protocolExpectation(o.URL)
	if err != nil {
		return engine.RequestQuery{}, err
	}
	method, err := protocolExpectation(o.Method)
	if err != nil {
		return engine.RequestQuery{}, err
	}
	resource, err := protocolExpectation(o.ResourceType)
	return engine.RequestQuery{URL: url, Method: method, ResourceType: resource}, err
}
func responseQueryFromProtocol(o protocol.EventfulOperation) (engine.ResponseQuery, error) {
	url, err := protocolExpectation(o.URL)
	if err != nil {
		return engine.ResponseQuery{}, err
	}
	status, err := protocolExpectation(o.State)
	return engine.ResponseQuery{URL: url, Status: status}, err
}

func decodeOverride(w protocol.WireNetworkOverride) (engine.RequestOverride, engine.ResponseOverride, error) {
	headers := map[string]string{}
	for _, header := range w.Headers {
		headers[header.Name] = header.Value
	}
	request := engine.RequestOverride{URL: w.URL, Method: w.Method, Headers: headers}
	entries := make([]engine.HeaderEntry, len(w.Headers))
	for index, header := range w.Headers {
		entries[index] = engine.HeaderEntry{Name: header.Name, Value: header.Value}
	}
	response := engine.ResponseOverride{Status: w.Status, HeaderEntries: entries}
	if w.BodyBase64 != nil {
		body, err := decodeBoundedBody(*w.BodyBase64)
		if err != nil {
			return request, response, err
		}
		request.Body = &body
		response.Body = &body
	}
	return request, response, nil
}

func decodeBoundedBody(value string) ([]byte, error) {
	if len(value) > base64.StdEncoding.EncodedLen(int(protocol.MaxDecodedBodySize)) {
		return nil, protocol.NewError(protocol.CodeInvalidArgument, fmt.Sprintf("decoded body exceeds limit %d", protocol.MaxDecodedBodySize))
	}
	body, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, protocol.NewError(protocol.CodeInvalidArgument, "bodyBase64 must be valid base64")
	}
	if int64(len(body)) > protocol.MaxDecodedBodySize {
		return nil, protocol.NewError(protocol.CodeInvalidArgument, fmt.Sprintf("decoded body size %d exceeds limit %d", len(body), protocol.MaxDecodedBodySize))
	}
	return body, nil
}
func networkHandlerOptionsFromProtocol(o protocol.EventfulOperation) (engine.NetworkHandlerOptions, error) {
	if o.URL == nil {
		return engine.NetworkHandlerOptions{}, protocol.NewError(protocol.CodeInvalidArgument, "url expectation is required")
	}
	url, err := expectationFromProtocol(*o.URL)
	if err != nil {
		return engine.NetworkHandlerOptions{}, err
	}
	request, response, err := decodeOverride(o.Override)
	if err != nil {
		return engine.NetworkHandlerOptions{}, err
	}
	options := engine.NetworkHandlerOptions{URL: url, Callsite: o.Callsite, ResponseBodyLimit: o.MaxBodyBytes, TransformTimeout: time.Duration(o.TransformTimeoutMS) * time.Millisecond}
	switch o.Action {
	case "fulfill":
		options.Fulfill = &response
	case "abort":
		options.Abort = true
	case "request":
		options.Request = &request
	case "response":
		options.Response = &response
	case "callback":
		options.Transform = func(ctx context.Context, intercepted engine.InterceptedResponse) (engine.ResponseOverride, error) {
			payload := map[string]any{"url": intercepted.URL, "status": intercepted.Status, "headers": engineHeaderEntriesToWire(intercepted.HeaderEntries, intercepted.Headers), "bodyBase64": base64.StdEncoding.EncodeToString(intercepted.Body)}
			wire, invokeErr := o.InvokeCallback(ctx, protocol.CallbackInvocation{CallbackID: o.CallbackID, Payload: payload})
			if invokeErr != nil {
				return engine.ResponseOverride{}, invokeErr
			}
			_, result, decodeErr := decodeOverride(wire)
			return result, decodeErr
		}
	default:
		return engine.NetworkHandlerOptions{}, protocol.NewError(protocol.CodeInvalidArgument, "unsupported network handler action")
	}
	return options, nil
}

func dialogToWire(v engine.Dialog) map[string]any {
	return map[string]any{"type": v.Type, "message": v.Message, "defaultPrompt": v.DefaultPrompt, "accepted": v.Accepted, "promptText": v.PromptText, "autoHandled": v.AutoHandled}
}
func dialogsToWire(values []engine.Dialog) []map[string]any {
	result := make([]map[string]any, len(values))
	for i, value := range values {
		result[i] = dialogToWire(value)
	}
	return result
}
func warningsToWire(values []engine.Warning) []map[string]any {
	result := make([]map[string]any, len(values))
	for i, value := range values {
		result[i] = warningToWire(value)
	}
	return result
}

func warningToWire(value engine.Warning) map[string]any {
	return map[string]any{"code": value.Code, "message": value.Message, "dialog": dialogToWire(value.Dialog), "generation": value.Generation}
}
func downloadToWire(v engine.Download) map[string]any {
	result := map[string]any{"id": v.ID, "url": v.URL, "filename": v.Filename, "state": v.State, "receivedBytes": v.ReceivedBytes, "totalBytes": v.TotalBytes, "startedAt": v.StartedAt.UnixMilli()}
	if !v.CompletedAt.IsZero() {
		result["completedAt"] = v.CompletedAt.UnixMilli()
	}
	return result
}
func downloadsToWire(values []engine.Download) []map[string]any {
	result := make([]map[string]any, len(values))
	for i, value := range values {
		result[i] = downloadToWire(value)
	}
	return result
}
func requestToWire(v engine.Request) map[string]any {
	return map[string]any{"url": v.URL, "method": v.Method, "headers": headerEntriesToWire(v.Headers), "resourceType": v.ResourceType}
}
func requestsToWire(values []engine.Request) []map[string]any {
	result := make([]map[string]any, len(values))
	for i, value := range values {
		result[i] = requestToWire(value)
	}
	return result
}
func responsesToWire(values []engine.Response) []map[string]any {
	result := make([]map[string]any, len(values))
	for i, v := range values {
		result[i] = map[string]any{"url": v.URL, "status": v.Status, "headers": headerEntriesToWire(v.Headers), "resourceType": v.ResourceType}
	}
	return result
}
func heldResponseToWire(v engine.HeldResponse) map[string]any {
	return map[string]any{"id": v.ID, "url": v.URL, "status": v.Status, "headers": engineHeaderEntriesToWire(v.HeaderEntries, v.Headers), "bodyBase64": base64.StdEncoding.EncodeToString(v.Body)}
}
func engineHeaderEntriesToWire(entries []engine.HeaderEntry, fallback map[string]string) []protocol.WireHeaderEntry {
	if len(entries) == 0 {
		return headerEntriesToWire(fallback)
	}
	result := make([]protocol.WireHeaderEntry, len(entries))
	for index, entry := range entries {
		result[index] = protocol.WireHeaderEntry{Name: entry.Name, Value: entry.Value}
	}
	return result
}
func headerEntriesToWire(headers map[string]string) []protocol.WireHeaderEntry {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]protocol.WireHeaderEntry, 0, len(headers))
	for _, name := range names {
		result = append(result, protocol.WireHeaderEntry{Name: name, Value: headers[name]})
	}
	return result
}
func networkStateFromWire(v protocol.WireNetworkState) engine.NetworkState {
	return engine.NetworkState{Offline: v.Offline, Latency: time.Duration(v.LatencyMS) * time.Millisecond, DownloadThroughput: v.DownloadThroughput, UploadThroughput: v.UploadThroughput, ConnectionType: engine.NetworkConnectionType(v.ConnectionType)}
}
func networkStateToWire(v engine.NetworkState) map[string]any {
	return map[string]any{"offline": v.Offline, "latencyMs": v.Latency.Milliseconds(), "downloadThroughput": v.DownloadThroughput, "uploadThroughput": v.UploadThroughput, "connectionType": v.ConnectionType}
}
func ownerToWire(v engine.NetworkOwnerProvenance) map[string]any {
	return map[string]any{"kind": v.Kind, "id": v.ID, "callsite": v.Callsite, "count": v.Count, "shadowed": v.Shadowed}
}
func shadowsToWire(values []engine.NetworkShadowDiagnostic) []map[string]any {
	result := make([]map[string]any, len(values))
	for i, v := range values {
		shadowed := make([]map[string]any, len(v.Shadowed))
		for j, owner := range v.Shadowed {
			shadowed[j] = ownerToWire(owner)
		}
		result[i] = map[string]any{"url": v.URL, "stage": v.Stage, "winner": ownerToWire(v.Winner), "shadowed": shadowed}
	}
	return result
}

type engineBackend struct {
	browser            *engine.Browser
	launch             protocol.WireLaunchMetadata
	visual             engine.VisualOptions
	maxScreenshotBytes int
	debug              *debugHub
}

type debugHub struct {
	mu          sync.Mutex
	next        uint64
	subscribers map[uint64]*debugSubscriber
	closed      bool
}

type debugSubscriber struct {
	events   chan protocol.SessionEvent
	dropped  uint64
	reported uint64
}

func newDebugHub() *debugHub { return &debugHub{subscribers: map[uint64]*debugSubscriber{}} }
func (h *debugHub) publish(entry engine.DebugEntry) {
	if h == nil {
		return
	}
	payload := map[string]any{"timestamp": entry.Timestamp, "direction": entry.Direction, "message": entry.Message, "truncated": entry.Truncated}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	for _, subscriber := range h.subscribers {
		if subscriber.dropped > subscriber.reported {
			count := subscriber.dropped - subscriber.reported
			select {
			case subscriber.events <- protocol.SessionEvent{Type: "eventsDropped", Payload: map[string]any{"code": "EVENTS_DROPPED", "message": fmt.Sprintf("%d debug events were dropped", count), "details": map[string]any{"count": count}}}:
				subscriber.reported = subscriber.dropped
			default:
			}
		}
		select {
		case subscriber.events <- protocol.SessionEvent{Type: "debug", Payload: payload}:
		default:
			subscriber.dropped++
		}
	}
}
func (h *debugHub) subscribe() (*debugHubSubscription, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, protocol.NewError(protocol.CodeDriverClosed, "debug stream is closed")
	}
	h.next++
	events := make(chan protocol.SessionEvent, 256)
	h.subscribers[h.next] = &debugSubscriber{events: events}
	return &debugHubSubscription{hub: h, id: h.next, events: events}, nil
}
func (h *debugHub) close() {
	if h == nil {
		return
	}
	h.mu.Lock()
	if !h.closed {
		h.closed = true
		for id, subscriber := range h.subscribers {
			close(subscriber.events)
			delete(h.subscribers, id)
		}
	}
	h.mu.Unlock()
}

type debugHubSubscription struct {
	hub    *debugHub
	id     uint64
	events <-chan protocol.SessionEvent
	once   sync.Once
}

func (s *debugHubSubscription) Events() <-chan protocol.SessionEvent { return s.events }
func (s *debugHubSubscription) Close() error {
	s.once.Do(func() {
		s.hub.mu.Lock()
		if subscriber, ok := s.hub.subscribers[s.id]; ok {
			delete(s.hub.subscribers, s.id)
			close(subscriber.events)
		}
		s.hub.mu.Unlock()
	})
	return nil
}

func (b *engineBackend) SubscribeDebug() (protocol.EventSubscription, error) {
	if b.debug == nil {
		return nil, protocol.NewError(protocol.CodeInvalidArgument, "debug logging is disabled")
	}
	return b.debug.subscribe()
}

func launchMetadataToWire(value engine.LaunchMetadata) protocol.WireLaunchMetadata {
	arguments := make([]string, len(value.Arguments))
	copy(arguments, value.Arguments)
	return protocol.WireLaunchMetadata{Mode: string(value.Mode), ExecutablePath: value.ExecutablePath, Arguments: arguments, Width: value.WindowWidth, Height: value.WindowHeight, Attached: value.Attached, AutoInstalled: value.AutoInstalled}
}

func launchMetadataForHost(value engine.LaunchMetadata) hostLaunchMetadata {
	arguments := make([]string, len(value.Arguments))
	copy(arguments, value.Arguments)
	result := hostLaunchMetadata{Mode: string(value.Mode), ExecutablePath: value.ExecutablePath, ChromeArgs: arguments, AutoInstalled: value.AutoInstalled}
	result.WindowSize.Width, result.WindowSize.Height = value.WindowWidth, value.WindowHeight
	return result
}

func (b *engineBackend) LaunchMetadata() protocol.WireLaunchMetadata {
	if b.launch.Attached {
		value := b.launch
		value.Arguments = make([]string, len(b.launch.Arguments))
		copy(value.Arguments, b.launch.Arguments)
		return value
	}
	return launchMetadataToWire(b.browser.LaunchMetadata())
}

func (b *engineBackend) OpenSession(ctx context.Context) (protocol.Session, error) {
	session, err := b.browser.OpenSession(ctx)
	if err != nil {
		return nil, engineRPCError(err)
	}
	return b.wrap(session, ""), nil
}

func (b *engineBackend) wrap(session *engine.Session, frameURL string) *engineSession {
	return &engineSession{session: session, frameURL: frameURL, visual: b.visual, maxScreenshotBytes: b.maxScreenshotBytes}
}

func (b *engineBackend) Close() error {
	b.debug.close()
	return b.browser.Close()
}

type engineSession struct {
	session            *engine.Session
	frameURL           string
	visual             engine.VisualOptions
	maxScreenshotBytes int
}

type engineEventSubscription struct {
	events   chan protocol.SessionEvent
	stop     chan struct{}
	once     sync.Once
	closers  []func() error
	dropped  atomic.Uint64
	reported atomic.Uint64
}

func (s *engineEventSubscription) Events() <-chan protocol.SessionEvent { return s.events }
func (s *engineEventSubscription) Close() error {
	s.once.Do(func() {
		close(s.stop)
		for _, closeSubscription := range s.closers {
			_ = closeSubscription()
		}
	})
	return nil
}

func (s *engineSession) SubscribeEvents(types []string) (protocol.EventSubscription, error) {
	result := &engineEventSubscription{events: make(chan protocol.SessionEvent, 256), stop: make(chan struct{})}
	var wg sync.WaitGroup
	forward := func(kind string, receive func() (any, uint64, bool), sourceDropped func() uint64) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				payload, generation, ok := receive()
				if !ok {
					return
				}
				dropped := sourceDropped() + result.dropped.Load()
				if dropped > result.reported.Load() {
					select {
					case result.events <- protocol.SessionEvent{Type: "eventsDropped", Generation: generation, Payload: map[string]any{"type": kind, "count": dropped}}:
						result.reported.Store(dropped)
					case <-result.stop:
						return
					default:
					}
				}
				select {
				case result.events <- protocol.SessionEvent{Type: kind, Generation: generation, Payload: payload}:
				case <-result.stop:
					return
				default:
					result.dropped.Add(1)
				}
			}
		}()
	}
	for _, kind := range types {
		switch kind {
		case "console":
			subscription, err := s.session.SubscribeConsole(256)
			if err != nil {
				_ = result.Close()
				return nil, engineRPCError(err)
			}
			result.closers = append(result.closers, subscription.Close)
			forward("console", func() (any, uint64, bool) {
				message, ok := <-subscription.Events()
				return consoleMessageToWire(message), message.Generation, ok
			}, subscription.Dropped)
		case "warning":
			subscription, err := s.session.SubscribeWarnings(256)
			if err != nil {
				_ = result.Close()
				return nil, engineRPCError(err)
			}
			result.closers = append(result.closers, subscription.Close)
			forward("warning", func() (any, uint64, bool) {
				warning, ok := <-subscription.Events()
				return warningToWire(warning), warning.Generation, ok
			}, subscription.Dropped)
		default:
			_ = result.Close()
			return nil, protocol.NewError(protocol.CodeInvalidArgument, "session subscriptions support console and warning events")
		}
	}
	go func() {
		wg.Wait()
		close(result.events)
	}()
	return result, nil
}

func (s *engineSession) CaptureContextDiagnostics(ctx context.Context, options protocol.ContextDiagnosticsOptions) (protocol.ContextDiagnosticsResponse, error) {
	return s.captureContextDiagnostics(ctx, options, "")
}

func (s *engineSession) captureContextDiagnostics(ctx context.Context, options protocol.ContextDiagnosticsOptions, primaryScreenshotPrefix string) (protocol.ContextDiagnosticsResponse, error) {
	capture := engine.DiagnosticsCaptureOptions{Purpose: engine.DiagnosticsPurpose(options.Purpose), Name: options.Name, Screenshots: options.Screenshots, Outlines: options.Outlines, MaxBytes: options.MaxBytes, IncludeScreenshotBytes: options.IncludeScreenshotBytes, PrimaryScreenshotPrefix: primaryScreenshotPrefix}
	if options.Width > 0 {
		capture.Viewport = &engine.ViewportSize{Width: options.Width, Height: options.Height}
	}
	value, err := s.session.CaptureContextDiagnostics(ctx, capture)
	response := protocol.ContextDiagnosticsResponse{Purpose: string(value.Purpose), ArtifactDir: value.ArtifactDir, Tabs: make([]protocol.TabDiagnosticsResponse, len(value.Tabs))}
	for index, tab := range value.Tabs {
		errors := make([]protocol.DiagnosticsArtifactErrorResponse, len(tab.Errors))
		for errorIndex, artifactErr := range tab.Errors {
			errors[errorIndex] = protocol.DiagnosticsArtifactErrorResponse{Artifact: artifactErr.Artifact, Code: diagnosticsErrorCodeToWire(artifactErr.Code), Message: artifactErr.Message}
		}
		response.Tabs[index] = protocol.TabDiagnosticsResponse{TargetID: string(tab.TargetID), Title: tab.Title, ScreenshotPath: tab.ScreenshotPath, OutlinePath: tab.OutlinePath, DOMOutline: tab.DOMOutline, Errors: errors}
		if len(tab.Screenshot) > 0 {
			response.Tabs[index].ScreenshotBase64 = base64.StdEncoding.EncodeToString(tab.Screenshot)
		}
	}
	return response, engineRPCError(err)
}

func diagnosticsErrorCodeToWire(code engine.ErrorCode) string {
	switch code {
	case engine.CodeIO:
		return "IO_ERROR"
	case engine.CodeActionFailed:
		return "ACTION_FAILED"
	case engine.CodeDeadline:
		return "DEADLINE_EXCEEDED"
	}
	if protocolCode, ok := engineProtocolCodes[code]; ok {
		return string(protocolCode)
	}
	return string(protocol.CodeDriver)
}

func (s *engineSession) wrap(session *engine.Session, frameURL string) *engineSession {
	return &engineSession{session: session, frameURL: frameURL, visual: s.visual, maxScreenshotBytes: s.maxScreenshotBytes}
}

func (s *engineSession) Metadata() protocol.SessionMetadata {
	return protocol.SessionMetadata{ContextID: string(s.session.ContextID()), TargetID: string(s.session.TargetID()), OpenerID: string(s.session.OpenerID()), OwnsContext: s.session.OwnsContext(), Frame: s.frameURL != "", URL: s.frameURL}
}

func (s *engineSession) Tabs(ctx context.Context) ([]protocol.Session, error) {
	tabs, err := s.session.Tabs(ctx)
	if err != nil {
		return nil, engineRPCError(err)
	}
	result := make([]protocol.Session, len(tabs))
	for index, tab := range tabs {
		result[index] = s.wrap(tab, "")
	}
	return result, nil
}

func (s *engineSession) WaitForTab(ctx context.Context, query protocol.TabQuery, policy protocol.PollPolicy) (protocol.Session, error) {
	converted, err := tabQueryFromProtocol(query)
	if err != nil {
		return nil, err
	}
	tab, waitErr := s.session.WaitForTab(ctx, converted, pollPolicyFromProtocol(policy))
	if waitErr != nil {
		return nil, engineRPCError(waitErr)
	}
	return s.wrap(tab, ""), nil
}

func (s *engineSession) Frames(ctx context.Context) ([]protocol.Session, error) {
	frames, err := s.session.Frames(ctx)
	if err != nil {
		return nil, engineRPCError(err)
	}
	result := make([]protocol.Session, len(frames))
	for index, frame := range frames {
		result[index] = s.wrap(frame.Session, frame.URL())
	}
	return result, nil
}

func (s *engineSession) WaitForFrame(ctx context.Context, query protocol.FrameQuery, policy protocol.PollPolicy) (protocol.Session, error) {
	tabQuery, err := tabQueryFromProtocol(protocol.TabQuery{Title: query.Title, URL: query.URL, HasElement: query.HasElement})
	if err != nil {
		return nil, err
	}
	frame, waitErr := s.session.WaitForFrame(ctx, engine.FrameQuery{Title: tabQuery.Title, URL: tabQuery.URL, HasElement: tabQuery.HasElement}, pollPolicyFromProtocol(policy))
	if waitErr != nil {
		return nil, engineRPCError(waitErr)
	}
	return s.wrap(frame.Session, frame.URL()), nil
}

const defaultPollTimeout = 10 * time.Second
const assertionAttemptTimeout = time.Second

func (s *engineSession) Prepare(ctx context.Context) error { return s.session.Prepare(ctx) }
func (s *engineSession) Close() error                      { return s.session.Close() }
func (s *engineSession) NewTab(ctx context.Context) (protocol.Session, error) {
	tab, err := s.session.NewTab(ctx)
	if err != nil {
		return nil, engineRPCError(err)
	}
	return s.wrap(tab, ""), nil
}

func (s *engineSession) Execute(ctx context.Context, operation protocol.Operation) (protocol.Result, error) {
	started := time.Now()
	switch operation.Kind {
	case protocol.OperationScreenshot:
		return s.screenshot(ctx, operation, started)
	case protocol.OperationNavigate:
		return oneAttempt(started, s.session.NavigateWithStatus(ctx, operation.URL, operation.ExpectedStatus))
	case protocol.OperationSetCookies:
		cookies := make([]engine.Cookie, len(operation.Cookies))
		for i, cookie := range operation.Cookies {
			cookies[i] = engine.Cookie{Name: cookie.Name, Value: cookie.Value, Domain: cookie.Domain, Path: cookie.Path, Secure: cookie.Secure, HTTPOnly: cookie.HTTPOnly, SameSite: cookie.SameSite}
			if cookie.ExpiresUnix != 0 {
				cookies[i].Expires = time.UnixMilli(int64(cookie.ExpiresUnix * 1000))
			}
		}
		return oneAttempt(started, s.session.SetCookies(ctx, cookies))
	case protocol.OperationClick:
		selector, err := selectorFromProtocol(operation.Locator)
		if err != nil {
			return protocol.Result{}, err
		}
		return s.poll(ctx, operation, func(ctx context.Context) (engine.Observation, bool, error) {
			var err error
			if operation.Realistic {
				err = s.session.RealisticClick(ctx, selector)
			} else {
				err = s.session.Click(ctx, selector)
			}
			return engine.Observation{}, err == nil, err
		})
	case protocol.OperationSetValue:
		selector, err := selectorFromProtocol(operation.Locator)
		if err != nil {
			return protocol.Result{}, err
		}
		var value any
		if err := json.Unmarshal([]byte(operation.ValueJSON), &value); err != nil {
			return protocol.Result{}, protocol.NewError(protocol.CodeInvalidArgument, fmt.Sprintf("value_json: %v", err))
		}
		return s.poll(ctx, operation, func(ctx context.Context) (engine.Observation, bool, error) {
			var err error
			if operation.Realistic {
				err = s.session.RealisticSetValue(ctx, selector, value)
			} else {
				err = s.session.SetValue(ctx, selector, value)
			}
			return engine.Observation{}, err == nil, err
		})
	case protocol.OperationType:
		selector, err := selectorFromProtocol(operation.Locator)
		if err != nil {
			return protocol.Result{}, err
		}
		return s.poll(ctx, operation, func(ctx context.Context) (engine.Observation, bool, error) {
			err := s.session.Type(ctx, selector, operation.Keys, operation.Realistic)
			return engine.Observation{}, err == nil, err
		})
	case protocol.OperationSendKeys:
		return oneAttempt(started, s.session.SendKeys(ctx, operation.Keys))
	case protocol.OperationSetWindowSize:
		return oneAttempt(started, s.session.SetWindowSize(ctx, operation.Width, operation.Height))
	case protocol.OperationSetUpload:
		selector, err := selectorFromProtocol(operation.Locator)
		if err != nil {
			return protocol.Result{}, err
		}
		return s.poll(ctx, operation, func(ctx context.Context) (engine.Observation, bool, error) {
			observation, err := s.session.SetUpload(ctx, selector, operation.Paths)
			return observation, observation.Found != nil && *observation.Found, err
		})
	case protocol.OperationDragTo:
		source, err := selectorFromProtocol(operation.Locator)
		if err != nil {
			return protocol.Result{}, err
		}
		targetSelector, err := selectorFromProtocol(operation.Target)
		if err != nil {
			return protocol.Result{}, err
		}
		return s.poll(ctx, operation, func(ctx context.Context) (engine.Observation, bool, error) {
			mode := engine.Fast
			if operation.Realistic {
				mode = engine.Realistic
			}
			err := s.session.DragWith(ctx, source, targetSelector, mode)
			return engine.Observation{}, err == nil, err
		})
	case protocol.OperationAddInitScript:
		return oneAttempt(started, s.session.AddInitScript(ctx, operation.Expression))
	case protocol.OperationActivate:
		return oneAttempt(started, s.session.Activate(ctx))
	case protocol.OperationHoldResponse:
		expectation, err := expectationFromProtocol(operation.Expectation)
		if err != nil {
			return protocol.Result{}, err
		}
		holdID, err := s.session.HoldResponse(ctx, expectation)
		if err != nil {
			return protocol.Result{}, engineRPCError(err)
		}
		return observedResult(started, map[string]any{"holdId": holdID}), nil
	case protocol.OperationAwaitResponseHold:
		response, err := s.session.AwaitResponseHold(ctx, operation.HoldID)
		if err != nil {
			return protocol.Result{}, engineRPCError(err)
		}
		return observedResult(started, map[string]any{"url": response.URL, "status": response.Status}), nil
	case protocol.OperationReleaseResponseHold:
		return oneAttempt(started, s.session.ReleaseResponseHold(ctx, operation.HoldID))
	case protocol.OperationEvaluate:
		script, err := evaluationScript(operation.Expression, operation.ArgumentsJSON, operation.Invoke)
		if err != nil {
			return protocol.Result{}, err
		}
		var value any
		if operation.AwaitPromise {
			value, err = s.session.EvaluateAsync(ctx, script)
		} else {
			value, err = s.session.Evaluate(ctx, script)
		}
		if err != nil {
			return protocol.Result{}, engineRPCError(err)
		}
		return observedResult(started, value), nil
	case protocol.OperationAssert:
		assertion, err := s.assertion(operation.Assertion)
		if err != nil {
			return protocol.Result{}, err
		}
		return s.poll(ctx, operation, assertion)
	case protocol.OperationDOM:
		assertion, err := s.domAssertion(operation.DOM)
		if err != nil {
			return protocol.Result{}, err
		}
		return s.poll(ctx, operation, assertion)
	case protocol.OperationLifecycle:
		return s.lifecycle(ctx, operation)
	default:
		return protocol.Result{}, protocol.NewError(protocol.CodeInvalidArgument, "unsupported operation")
	}
}

func (s *engineSession) screenshot(ctx context.Context, operation protocol.Operation, started time.Time) (protocol.Result, error) {
	request := operation.Screenshot
	maxBytes := request.MaxBytes
	configuredMax := s.maxScreenshotBytes
	if configuredMax <= 0 {
		configuredMax = engine.DefaultMaxScreenshotBytes
	}
	if maxBytes == 0 {
		maxBytes = configuredMax
	}
	if maxBytes > configuredMax {
		return protocol.Result{}, protocol.NewError(protocol.CodeInvalidArgument, fmt.Sprintf("maxBytes %d exceeds daemon limit %d", maxBytes, configuredMax))
	}
	masks := make([]engine.Selector, len(request.Masks))
	for index, locator := range request.Masks {
		selector, err := selectorFromProtocol(locator)
		if err != nil {
			return protocol.Result{}, err
		}
		masks[index] = selector
	}
	var target engine.ScreenshotTarget
	var selector engine.Selector
	if request.Target.Kind == protocol.ScreenshotElement {
		var err error
		selector, err = selectorFromProtocol(request.Target.Locator)
		if err != nil {
			return protocol.Result{}, err
		}
		target = engine.ElementScreenshotTarget(selector)
	} else {
		target = engine.PageScreenshotTarget()
	}
	if request.Kind == protocol.ScreenshotCapture {
		captureOptions := engine.ScreenshotCaptureOptions{Masks: masks, Animated: request.Animated, ColorScheme: request.ColorScheme, MaxBytes: maxBytes}
		var shot engine.Screenshot
		var err error
		if request.Target.Kind == protocol.ScreenshotElement {
			shot, err = s.session.CaptureElementScreenshot(ctx, selector, captureOptions)
		} else {
			shot, err = s.session.CapturePageScreenshot(ctx, captureOptions)
		}
		if err != nil {
			return protocol.Result{}, engineRPCError(err)
		}
		if len(shot.PNG) > maxBytes {
			return protocol.Result{}, protocol.NewError(protocol.CodeDriver, fmt.Sprintf("captured screenshot exceeds decoded limit %d", maxBytes))
		}
		capture := &protocol.ScreenshotCaptureResult{Width: shot.Width, Height: shot.Height, FullyClipped: shot.FullyClipped, Vanished: shot.Vanished}
		if shot.Warning != "" {
			capture.Warnings = []string{shot.Warning}
		}
		if request.Output == protocol.ScreenshotPath {
			name := request.Name
			if name == "" {
				name = map[bool]string{true: "element", false: "page"}[request.Target.Kind == protocol.ScreenshotElement]
			}
			capture.ArtifactPath, err = s.session.WriteScreenshotArtifact(name, shot.PNG, maxBytes)
			if err != nil {
				return protocol.Result{}, engineRPCError(err)
			}
		} else {
			capture.PNGBase64 = base64.StdEncoding.EncodeToString(shot.PNG)
		}
		return protocol.Result{Matched: true, Attempts: 1, StartedAt: started, Elapsed: time.Since(started), Screenshot: capture}, nil
	}
	visualOptions := s.visual
	visualOptions.Masks = masks
	visualOptions.Animated = request.Animated
	visualOptions.ColorSchemes = append([]string(nil), request.ColorSchemes...)
	visualOptions.MaxBytes = maxBytes
	if request.PixelToleranceSet {
		visualOptions.Tolerance.PixelFraction = request.PixelTolerance
	}
	if request.ChannelToleranceSet {
		visualOptions.Tolerance.ChannelDelta = request.ChannelTolerance
	}
	if visualOptions.Update {
		result, err := s.session.CompareScreenshot(ctx, request.Name, target, visualOptions)
		wire := visualResultToWire(result, 1, time.Since(started))
		return protocol.Result{Matched: result.Match, Attempts: 1, StartedAt: started, Elapsed: time.Since(started), Visual: wire}, visualRPCError(err)
	}
	writeArtifacts := false
	visualOptions.WriteArtifacts = &writeArtifacts
	result, pollErr := engine.Poll(ctx, withDefaultPollTimeout(pollPolicyFromProtocol(operation.Poll)), func(attemptCtx context.Context) (engine.Observation, bool, error) {
		visual, err := s.session.CompareScreenshot(attemptCtx, request.Name, target, visualOptions)
		return engine.Observation{Value: visual}, visual.Match, err
	})
	final, _ := result.Final.Value.(engine.VisualResult)
	if pollErr != nil && !engine.IsFatal(pollErr) && ctx.Err() == nil {
		// Reporting re-runs the whole capture and comparison so the artifacts get written.  On a tall
		// page that costs as much as a poll attempt did, while the client grants only a fixed slack
		// past the poll timeout - so running it unbounded turned a diagnosable mismatch into a bare
		// "request timed out" carrying no visual result at all.  Bound it against the request
		// deadline and keep what the poll already observed if it cannot finish in time.
		reportCtx, cancelReport := context.WithTimeout(ctx, diagnosticsBudget(ctx, time.Now()))
		writeArtifacts = true
		reported, reportErr := s.session.CompareScreenshot(reportCtx, request.Name, target, visualOptions)
		writeArtifacts = false
		cancelReport()
		switch {
		case reportErr != nil && engine.IsFatal(reportErr):
			pollErr = reportErr
		case reported.Match:
			// The page settled between the final attempt and this one, so there is nothing left to
			// show.  Adopting it would report match:true on the very result the client is about to
			// raise as a mismatch.
		case reported.Schemes != nil || len(reported.Warnings) > 0:
			final = reported
		}
	}
	wire := visualResultToWire(final, uint32(result.AttemptCount), time.Since(started))
	converted := protocol.Result{Matched: final.Match && pollErr == nil, Attempts: uint32(result.AttemptCount), StartedAt: started, Elapsed: time.Since(started), Visual: wire}
	if pollErr == nil {
		return converted, nil
	}
	if engine.IsFatal(pollErr) || ctx.Err() != nil {
		return converted, visualRPCError(pollErr)
	}
	return converted, nil
}

func visualRPCError(err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	for _, marker := range []string{"no screenshot baseline", "screenshot baseline", "baseline name"} {
		if strings.Contains(message, marker) {
			return protocol.NewError(protocol.CodeVisualBaseline, message)
		}
	}
	return engineRPCError(err)
}

func visualResultToWire(value engine.VisualResult, attempts uint32, elapsed time.Duration) *protocol.WireVisualResult {
	wire := &protocol.WireVisualResult{Match: value.Match, Updated: value.Updated, AttemptCount: attempts, ElapsedMS: elapsed.Milliseconds(), Warnings: append([]string{}, value.Warnings...), Schemes: make([]protocol.WireVisualSchemeResult, len(value.Schemes))}
	for index, scheme := range value.Schemes {
		status := string(scheme.UpdateDisposition)
		if status == "" {
			switch {
			case scheme.Match:
				status = "matched"
			case scheme.ActualPath != "" && scheme.DiffPath == "" && scheme.Diff.TotalPixels == 0:
				status = "missing"
			default:
				status = "mismatched"
			}
		}
		converted := protocol.WireVisualSchemeResult{Scheme: scheme.Scheme, Status: status, Match: scheme.Match, BaselinePath: scheme.BaselinePath, ActualPath: scheme.ActualPath, DiffPath: scheme.DiffPath, Diagnosis: scheme.Diagnosis, Warning: scheme.Warning, UpdateSummary: scheme.UpdateSummary}
		if screenshotDiffPresent(scheme.Diff) {
			converted.Diff = screenshotDiffToWire(scheme.Diff)
		}
		if scheme.PreviousDiff != nil {
			converted.PreviousDiff = screenshotDiffToWire(*scheme.PreviousDiff)
		}
		wire.Schemes[index] = converted
	}
	return wire
}

func screenshotDiffPresent(diff engine.ScreenshotDiff) bool {
	return diff.Match || diff.DimensionMismatch || diff.TotalPixels != 0 || diff.DifferingPixels != 0
}

func screenshotDiffToWire(diff engine.ScreenshotDiff) *protocol.WireVisualDiff {
	regions := make([]protocol.WireVisualRegion, len(diff.Regions))
	for index, region := range diff.Regions {
		regions[index] = protocol.WireVisualRegion{Rect: protocol.WireScreenshotRect{Min: protocol.WireScreenshotPoint{X: region.Rect.Min.X, Y: region.Rect.Min.Y}, Max: protocol.WireScreenshotPoint{X: region.Rect.Max.X, Y: region.Rect.Max.Y}}, DifferingPixels: region.Count}
	}
	wire := &protocol.WireVisualDiff{Match: diff.Match, DimensionMismatch: diff.DimensionMismatch, Baseline: protocol.WireScreenshotBounds{Width: diff.BaselineBounds.Dx(), Height: diff.BaselineBounds.Dy()}, Actual: protocol.WireScreenshotBounds{Width: diff.ActualBounds.Dx(), Height: diff.ActualBounds.Dy()}, TotalPixels: diff.TotalPixels, DifferingPixels: diff.DifferingPixels, Fraction: diff.Fraction, MaxChannelDelta: diff.MaxChannelDelta, Regions: regions, RegionCount: diff.RegionCount, Shifted: diff.Shifted, Scattered: diff.Scattered, RasterizationLikely: diff.RasterizationLikely, Unchanged: diff.Unchanged}
	if diff.Shifted {
		wire.Shift = &protocol.WireScreenshotPoint{X: diff.Shift.X, Y: diff.Shift.Y}
	}
	return wire
}

func tabQueryFromProtocol(query protocol.TabQuery) (engine.TabQuery, error) {
	result := engine.TabQuery{SpawnedOnly: query.SpawnedOnly}
	if query.Title != nil {
		value, err := expectationFromProtocol(*query.Title)
		if err != nil {
			return engine.TabQuery{}, err
		}
		result.Title = &value
	}
	if query.URL != nil {
		value, err := expectationFromProtocol(*query.URL)
		if err != nil {
			return engine.TabQuery{}, err
		}
		result.URL = &value
	}
	if query.HasElement != nil {
		value, err := selectorFromProtocol(*query.HasElement)
		if err != nil {
			return engine.TabQuery{}, err
		}
		result.HasElement = &value
	}
	return result, nil
}

func (s *engineSession) lifecycle(ctx context.Context, operation protocol.Operation) (protocol.Result, error) {
	started := time.Now()
	lifecycle := operation.Lifecycle
	switch lifecycle.Kind {
	case protocol.LifecycleGetCookies:
		cookies, err := s.session.GetCookies(ctx)
		if err != nil {
			return protocol.Result{}, engineRPCError(err)
		}
		return observedResult(started, cookiesToWire(cookies)), nil
	case protocol.LifecycleClearCookies:
		return oneAttempt(started, s.session.ClearCookies(ctx))
	case protocol.LifecycleCookieQuery:
		return s.poll(ctx, operation, s.cookieAssertion(lifecycle))
	case protocol.LifecycleStorageSet, protocol.LifecycleStorageGet, protocol.LifecycleStorageGetAll, protocol.LifecycleStorageRemove, protocol.LifecycleStorageClear, protocol.LifecycleStorageLength:
		return s.storageOperation(ctx, operation, started)
	case protocol.LifecycleWaitForDefined:
		result, err := s.session.WaitForDefined(ctx, lifecycle.Expression, withDefaultPollTimeout(pollPolicyFromProtocol(operation.Poll)))
		if err != nil {
			return pollResult(result, false), engineRPCError(err)
		}
		return pollResult(result, true), nil
	case protocol.LifecycleURL:
		if lifecycle.Expectation.Kind != 0 {
			return s.poll(ctx, operation, s.lifecycleValueAssertion(lifecycle, func(readCtx context.Context) (any, error) {
				value, err := s.session.URL(readCtx)
				return value.Value, err
			}))
		}
		value, err := s.session.URL(ctx)
		if err != nil {
			return protocol.Result{}, engineRPCError(err)
		}
		return observedResult(started, value.Value), nil
	case protocol.LifecycleTitle:
		if lifecycle.Expectation.Kind != 0 {
			return s.poll(ctx, operation, s.lifecycleValueAssertion(lifecycle, func(readCtx context.Context) (any, error) { return s.session.Title(readCtx) }))
		}
		value, err := s.session.Title(ctx)
		if err != nil {
			return protocol.Result{}, engineRPCError(err)
		}
		return observedResult(started, value), nil
	case protocol.LifecycleWindowSize:
		if lifecycle.Expectation.Kind != 0 {
			return s.poll(ctx, operation, s.lifecycleValueAssertion(lifecycle, func(readCtx context.Context) (any, error) {
				width, height, err := s.session.WindowSize(readCtx)
				return map[string]int{"width": width, "height": height}, err
			}))
		}
		width, height, err := s.session.WindowSize(ctx)
		if err != nil {
			return protocol.Result{}, engineRPCError(err)
		}
		return observedResult(started, map[string]int{"width": width, "height": height}), nil
	case protocol.LifecycleOutline:
		value, err := s.session.Outline(ctx)
		if err != nil {
			return protocol.Result{}, engineRPCError(err)
		}
		return observedResult(started, value), nil
	case protocol.LifecycleAccessibilityOutline:
		value, err := s.session.AccessibilityOutline(ctx)
		if err != nil {
			return protocol.Result{}, engineRPCError(err)
		}
		return observedResult(started, value), nil
	case protocol.LifecycleConsoleMessages:
		if lifecycle.Expectation.Kind != 0 {
			return s.poll(ctx, operation, func(context.Context) (engine.Observation, bool, error) {
				expected, convertErr := expectationFromProtocol(lifecycle.Expectation)
				if convertErr != nil {
					return engine.Observation{}, false, engine.Fatal(convertErr)
				}
				for _, message := range s.session.ConsoleMessages() {
					if lifecycle.Key != "" && lifecycle.Key != message.Type {
						continue
					}
					matched, matchErr := engine.MatchExpectation(message.Text, expected)
					if matchErr != nil {
						return engine.Observation{}, false, engine.Fatal(matchErr)
					}
					if matched {
						return engine.Observation{Value: consoleMessageToWire(message)}, true, nil
					}
				}
				return engine.Observation{Value: nil}, false, nil
			})
		}
		return observedResult(started, consoleMessagesToWire(s.session.ConsoleMessages())), nil
	case protocol.LifecycleSetDeviceMetrics:
		return oneAttempt(started, s.session.SetDeviceMetrics(ctx, engine.DeviceMetrics{Width: lifecycle.Width, Height: lifecycle.Height, DeviceScaleFactor: lifecycle.DeviceScaleFactor, Mobile: lifecycle.Mobile}))
	case protocol.LifecycleClearDeviceMetrics:
		return oneAttempt(started, s.session.ClearDeviceMetrics(ctx))
	case protocol.LifecycleSetGeolocation:
		return oneAttempt(started, s.session.SetGeolocation(ctx, engine.Geolocation{Latitude: lifecycle.Latitude, Longitude: lifecycle.Longitude, Accuracy: lifecycle.Accuracy}))
	case protocol.LifecycleClearGeolocation:
		return oneAttempt(started, s.session.ClearGeolocation(ctx))
	case protocol.LifecycleSetPermissions:
		permissions := map[engine.Permission]engine.PermissionState{}
		for name, state := range lifecycle.Permissions {
			permissions[engine.Permission(name)] = engine.PermissionState(state)
		}
		return oneAttempt(started, s.session.SetPermissions(ctx, lifecycle.Origin, permissions))
	case protocol.LifecycleResetPermissions:
		return oneAttempt(started, s.session.ResetPermissions(ctx))
	case protocol.LifecycleSetLocale:
		return oneAttempt(started, s.session.SetLocale(ctx, lifecycle.Locale))
	case protocol.LifecycleClearLocale:
		return oneAttempt(started, s.session.ClearLocale(ctx))
	case protocol.LifecycleSetTimezone:
		return oneAttempt(started, s.session.SetTimezone(ctx, lifecycle.Timezone))
	case protocol.LifecycleClearTimezone:
		return oneAttempt(started, s.session.ClearTimezone(ctx))
	case protocol.LifecycleSetMedia:
		return oneAttempt(started, s.session.SetMedia(ctx, engine.Media{Type: lifecycle.MediaType, ColorScheme: lifecycle.ColorScheme, ReducedMotion: lifecycle.ReducedMotion}))
	case protocol.LifecycleClearMedia:
		return oneAttempt(started, s.session.ClearMedia(ctx))
	default:
		return protocol.Result{}, protocol.NewError(protocol.CodeInvalidArgument, "unsupported lifecycle operation")
	}
}

func consoleMessageToWire(message engine.ConsoleMessage) map[string]any {
	stack := make([]map[string]any, len(message.Stack))
	for index, frame := range message.Stack {
		stack[index] = map[string]any{"url": frame.URL, "functionName": frame.FunctionName, "line": frame.Line, "column": frame.Column}
	}
	return map[string]any{"type": message.Type, "text": message.Text, "args": message.Args, "timestamp": message.Timestamp, "stack": stack, "generation": message.Generation}
}

func consoleMessagesToWire(messages []engine.ConsoleMessage) []map[string]any {
	result := make([]map[string]any, len(messages))
	for index, message := range messages {
		result[index] = consoleMessageToWire(message)
	}
	return result
}

func (s *engineSession) lifecycleValueAssertion(operation protocol.LifecycleOperation, read func(context.Context) (any, error)) engine.Assertion {
	return func(ctx context.Context) (engine.Observation, bool, error) {
		value, err := read(ctx)
		if err != nil {
			return engine.Observation{}, false, err
		}
		expected, convertErr := expectationFromProtocol(operation.Expectation)
		if convertErr != nil {
			return engine.Observation{}, false, engine.Fatal(convertErr)
		}
		matched, matchErr := engine.MatchExpectation(value, expected)
		return engine.Observation{Value: value}, matched, matchErr
	}
}

func cookiesToWire(cookies []engine.Cookie) []protocol.WireCookie {
	result := make([]protocol.WireCookie, len(cookies))
	for index, cookie := range cookies {
		result[index] = protocol.WireCookie{Name: cookie.Name, Value: cookie.Value, Domain: cookie.Domain, Path: cookie.Path, Secure: cookie.Secure, HTTPOnly: cookie.HTTPOnly, SameSite: cookie.SameSite, Session: cookie.Session}
		if !cookie.Session && !cookie.Expires.IsZero() {
			result[index].ExpiresUnix = float64(cookie.Expires.UnixMilli()) / 1000
		}
	}
	return result
}

func (s *engineSession) cookieAssertion(operation protocol.LifecycleOperation) engine.Assertion {
	return func(ctx context.Context) (engine.Observation, bool, error) {
		cookies, err := s.session.GetCookies(ctx)
		if err != nil {
			return engine.Observation{}, false, err
		}
		matched := make([]engine.Cookie, 0)
		for _, cookie := range cookies {
			ok, matchErr := cookieMatches(cookie, operation.Cookie)
			if matchErr != nil {
				return engine.Observation{}, false, engine.Fatal(matchErr)
			}
			if ok {
				matched = append(matched, cookie)
			}
		}
		if operation.Count {
			ok, matchErr := engine.MatchExpectation(len(matched), mustEngineExpectation(operation.Expectation))
			return engine.Observation{Value: len(matched)}, ok, matchErr
		}
		if len(matched) == 0 {
			return engine.Observation{Value: nil}, false, nil
		}
		return engine.Observation{Value: cookiesToWire(matched[:1])[0]}, true, nil
	}
}

func cookieMatches(cookie engine.Cookie, query protocol.CookieQuery) (bool, error) {
	for expectation, value := range map[*protocol.Expectation]any{query.Name: cookie.Name, query.Value: cookie.Value, query.Domain: cookie.Domain, query.Path: cookie.Path, query.SameSite: cookie.SameSite} {
		if expectation == nil {
			continue
		}
		converted, err := expectationFromProtocol(*expectation)
		if err != nil {
			return false, err
		}
		matched, err := engine.MatchExpectation(value, converted)
		if err != nil || !matched {
			return false, err
		}
	}
	if query.Secure != nil && cookie.Secure != *query.Secure {
		return false, nil
	}
	if query.HTTPOnly != nil && cookie.HTTPOnly != *query.HTTPOnly {
		return false, nil
	}
	return true, nil
}

func mustEngineExpectation(expectation protocol.Expectation) engine.Expectation {
	converted, _ := expectationFromProtocol(expectation)
	return converted
}

func (s *engineSession) storageOperation(ctx context.Context, operation protocol.Operation, started time.Time) (protocol.Result, error) {
	state := operation.Lifecycle
	area := engine.StorageArea(state.Area)
	storage := s.session.Storage(area)
	switch state.Kind {
	case protocol.LifecycleStorageSet:
		var value any
		if err := json.Unmarshal([]byte(state.ValueJSON), &value); err != nil {
			return protocol.Result{}, protocol.NewError(protocol.CodeInvalidArgument, err.Error())
		}
		return oneAttempt(started, storage.Set(ctx, state.Key, value))
	case protocol.LifecycleStorageRemove:
		return oneAttempt(started, storage.Remove(ctx, state.Key))
	case protocol.LifecycleStorageClear:
		return oneAttempt(started, storage.Clear(ctx))
	case protocol.LifecycleStorageGetAll:
		value, err := storage.GetAll(ctx)
		if err != nil {
			return protocol.Result{}, engineRPCError(err)
		}
		return observedResult(started, value), nil
	case protocol.LifecycleStorageLength:
		if state.Expectation.Kind != 0 {
			return s.poll(ctx, operation, func(attemptCtx context.Context) (engine.Observation, bool, error) {
				value, err := storage.Length(attemptCtx)
				if err != nil {
					return engine.Observation{}, false, err
				}
				expected, convertErr := expectationFromProtocol(state.Expectation)
				if convertErr != nil {
					return engine.Observation{}, false, engine.Fatal(convertErr)
				}
				matched, matchErr := engine.MatchExpectation(value, expected)
				return engine.Observation{Value: value}, matched, matchErr
			})
		}
		value, err := storage.Length(ctx)
		if err != nil {
			return protocol.Result{}, engineRPCError(err)
		}
		return observedResult(started, value), nil
	case protocol.LifecycleStorageGet:
		if state.Expectation.Kind != 0 {
			return s.poll(ctx, operation, func(attemptCtx context.Context) (engine.Observation, bool, error) {
				value, found, err := storage.Get(attemptCtx, state.Key)
				if err != nil {
					return engine.Observation{}, false, err
				}
				if !found {
					return engine.Observation{Value: nil}, false, nil
				}
				expected, convertErr := expectationFromProtocol(state.Expectation)
				if convertErr != nil {
					return engine.Observation{}, false, engine.Fatal(convertErr)
				}
				matched, matchErr := engine.MatchExpectation(value, expected)
				return engine.Observation{Value: value}, matched, matchErr
			})
		}
		value, found, err := storage.Get(ctx, state.Key)
		if err != nil {
			return protocol.Result{}, engineRPCError(err)
		}
		return observedResult(started, map[string]any{"value": value, "found": found}), nil
	}
	return protocol.Result{}, protocol.NewError(protocol.CodeInvalidArgument, "unsupported storage operation")
}

func (s *engineSession) domAssertion(operation protocol.DOMOperation) (engine.Assertion, error) {
	var selector, target, container engine.Selector
	var err error
	if operation.Kind != protocol.DOMSendKeys && operation.Kind != protocol.DOMClearSelection && operation.Kind != protocol.DOMNormalizeColor {
		selector, err = selectorFromProtocol(operation.Locator)
		if err != nil {
			return nil, err
		}
	}
	if operation.Target.Kind != 0 {
		target, err = selectorFromProtocol(operation.Target)
		if err != nil {
			return nil, err
		}
	}
	if operation.Container.Kind != 0 {
		container, err = selectorFromProtocol(operation.Container)
		if err != nil {
			return nil, err
		}
	}
	mode := engine.Fast
	if operation.Realistic {
		mode = engine.Realistic
	}
	modifiers, err := modifiersFromProtocol(operation.Modifiers)
	if err != nil {
		return nil, err
	}
	textModes := map[string]engine.TextMode{"INNER_TEXT": engine.InnerText, "TEXT_CONTENT": engine.TextContent, "NORMALIZED_TEXT": engine.NormalizedText}
	states := map[string]engine.ElementState{"visible": engine.StateVisible, "enabled": engine.StateEnabled, "clickable": engine.StateClickable, "checked": engine.StateChecked, "focused": engine.StateFocused}
	relations := map[string]engine.GeometryRelation{"above": engine.Above, "below": engine.Below, "leftOf": engine.LeftOf, "rightOf": engine.RightOf, "encloses": engine.Encloses, "overlaps": engine.Overlaps}
	names := make([]engine.NameSpec, len(operation.Names))
	plainNames := make([]string, len(operation.Names))
	for index, name := range operation.Names {
		plainNames[index] = name.Name
		if name.AllowMissing {
			names[index] = engine.OptionalName(name.Name)
		} else {
			names[index] = engine.RequiredName(name.Name)
		}
	}
	var value any
	if operation.Kind == protocol.DOMSetProperty {
		if err := json.Unmarshal([]byte(operation.ValueJSON), &value); err != nil {
			return nil, protocol.NewError(protocol.CodeInvalidArgument, fmt.Sprintf("valueJson: %v", err))
		}
	}
	arguments := []any{}
	if operation.ArgumentsJSON != "" {
		if err := json.Unmarshal([]byte(operation.ArgumentsJSON), &arguments); err != nil {
			return nil, protocol.NewError(protocol.CodeInvalidArgument, fmt.Sprintf("argumentsJson: %v", err))
		}
	}
	expectation := engine.Expectation{Kind: engine.ExpectAnything}
	if operation.Expectation.Kind != 0 {
		expectation, err = expectationFromProtocol(operation.Expectation)
		if err != nil {
			return nil, err
		}
	}
	return func(ctx context.Context) (engine.Observation, bool, error) {
		observation := engine.Observation{}
		var operationErr error
		switch operation.Kind {
		case protocol.DOMText:
			observation, operationErr = s.session.TextByMode(ctx, selector, textModes[operation.TextMode])
		case protocol.DOMTexts:
			observation, operationErr = s.session.Texts(ctx, selector, textModes[operation.TextMode])
		case protocol.DOMClasses:
			observation, operationErr = s.session.Classes(ctx, selector)
		case protocol.DOMClassesForEach:
			observation, operationErr = s.session.ClassesForEach(ctx, selector)
		case protocol.DOMDistinctAttributeCount:
			observation, operationErr = s.session.DistinctAttributeCount(ctx, selector, operation.Name)
		case protocol.DOMAttributes:
			observation, operationErr = s.session.Attributes(ctx, selector, names)
		case protocol.DOMAttributesForEach:
			observation, operationErr = s.session.AttributesForEach(ctx, selector, plainNames)
		case protocol.DOMJSONAttribute:
			observation, operationErr = s.session.JSONAttribute(ctx, selector, operation.Name)
		case protocol.DOMProperties:
			observation, operationErr = s.session.Properties(ctx, selector, names)
		case protocol.DOMPropertiesForEach:
			observation, operationErr = s.session.PropertiesForEach(ctx, selector, plainNames)
		case protocol.DOMPropertyForEach:
			observation, operationErr = s.session.PropertyForEach(ctx, selector, operation.Name)
		case protocol.DOMValues:
			observation, operationErr = s.session.Values(ctx, selector)
		case protocol.DOMState:
			state, ok := states[operation.State]
			if !ok {
				return observation, false, engine.Fatal(protocol.NewError(protocol.CodeInvalidArgument, "unsupported element state"))
			}
			observation, operationErr = s.session.State(ctx, selector, state)
		case protocol.DOMAllState:
			state, ok := states[operation.State]
			if !ok {
				return observation, false, engine.Fatal(protocol.NewError(protocol.CodeInvalidArgument, "unsupported all-element state"))
			}
			observation, operationErr = s.session.AllState(ctx, selector, state)
		case protocol.DOMSetProperty:
			scope := engine.FirstMatch
			if operation.All {
				scope = engine.AllMatches
			}
			operationErr = s.session.SetProperty(ctx, selector, operation.Name, value, scope)
		case protocol.DOMFocus:
			operationErr = s.session.Focus(ctx, selector)
		case protocol.DOMBlur:
			operationErr = s.session.Blur(ctx, selector)
		case protocol.DOMHover:
			operationErr = s.session.Hover(ctx, selector, mode)
		case protocol.DOMType:
			operationErr = s.session.TypeWith(ctx, selector, operation.Keys, engine.KeyboardOptions{Mode: mode, Modifiers: modifiers})
		case protocol.DOMSendKeys:
			operationErr = s.session.SendKeysWith(ctx, operation.Keys, modifiers)
		case protocol.DOMClick:
			button, buttonErr := buttonFromProtocol(operation.Button)
			if buttonErr != nil {
				return observation, false, engine.Fatal(buttonErr)
			}
			options := engine.ClickOptions{Mode: mode, Button: button, Count: operation.ClickCount, Modifiers: modifiers}
			if operation.HasOffset {
				options.Offset = &engine.Point{X: operation.OffsetX, Y: operation.OffsetY}
			}
			operationErr = s.session.ClickWith(ctx, selector, options)
		case protocol.DOMClickEach:
			operationErr = s.session.ClickEach(ctx, selector, mode)
		case protocol.DOMTap:
			options := engine.PointerOptions{Mode: mode, Modifiers: modifiers}
			if operation.HasOffset {
				options.Offset = &engine.Point{X: operation.OffsetX, Y: operation.OffsetY}
			}
			operationErr = s.session.Tap(ctx, selector, options)
		case protocol.DOMDrag:
			operationErr = s.session.DragWith(ctx, selector, target, mode)
		case protocol.DOMScrollIntoView:
			options := engine.ScrollIntoViewOptions{TopOffset: operation.TopOffset, HasTopOffset: operation.HasTopOffset}
			if operation.Container.Kind != 0 {
				options.Container = container
			}
			operationErr = s.session.ScrollIntoView(ctx, selector, options)
		case protocol.DOMScrollWheel:
			operationErr = s.session.ScrollWheel(ctx, selector, operation.DeltaX, operation.DeltaY, mode)
		case protocol.DOMSelect:
			operationErr = s.session.Select(ctx, selector, engine.Selection{Substring: operation.Substring, Occurrence: operation.Occurrence, Start: operation.Start, End: operation.End, Range: operation.Range})
		case protocol.DOMClearSelection:
			operationErr = s.session.ClearSelection(ctx)
		case protocol.DOMInvokeMethod:
			observation, operationErr = s.session.InvokeMethod(ctx, selector, operation.Method, arguments...)
		case protocol.DOMInvokeFunction:
			observation, operationErr = s.session.InvokeFunction(ctx, selector, operation.Expression, arguments...)
		case protocol.DOMInvokeMethodForEach:
			observation, operationErr = s.session.InvokeMethodForEach(ctx, selector, operation.Method, arguments...)
		case protocol.DOMInvokeFunctionForEach:
			observation, operationErr = s.session.InvokeFunctionForEach(ctx, selector, operation.Expression, arguments...)
		case protocol.DOMBoundingBox:
			var box engine.Box
			box, operationErr = s.session.BoundingBox(ctx, selector)
			observation.Value = box
		case protocol.DOMScrollOffset:
			var offset engine.ScrollOffset
			offset, operationErr = s.session.ScrollOffset(ctx, selector)
			observation.Value = offset
		case protocol.DOMOffsetWithin:
			var offset engine.Offset
			offset, operationErr = s.session.OffsetWithin(ctx, selector, target)
			observation.Value = offset
		case protocol.DOMRelativeBoxes:
			var boxes engine.BoxPair
			boxes, operationErr = s.session.RelativeBoxes(ctx, selector, target)
			observation.Value = boxes
		case protocol.DOMGeometryRelation:
			relation, ok := relations[operation.Relation]
			if !ok {
				return observation, false, engine.Fatal(protocol.NewError(protocol.CodeInvalidArgument, "unsupported geometry relation"))
			}
			observation, operationErr = s.session.GeometryRelation(ctx, selector, target, relation)
		case protocol.DOMGapBetween:
			var gap engine.BoxDelta
			gap, operationErr = s.session.GapBetween(ctx, selector, target)
			observation.Value = gap
		case protocol.DOMInViewport:
			observation, operationErr = s.session.InViewport(ctx, selector, operation.Fully)
		case protocol.DOMDocumentOrder:
			var order engine.DocumentOrder
			order, operationErr = s.session.DocumentOrder(ctx, selector, target)
			observation.Value = order
		case protocol.DOMComputedStyle:
			observation, operationErr = s.session.ComputedStyle(ctx, selector, operation.Name)
		case protocol.DOMComputedStyleNumber:
			observation, operationErr = s.session.ComputedStyleNumber(ctx, selector, operation.Name)
		case protocol.DOMNormalizeColor:
			observation, operationErr = s.session.NormalizeColor(ctx, operation.ValueJSON)
		default:
			return observation, false, engine.Fatal(protocol.NewError(protocol.CodeInvalidArgument, "unsupported DOM operation"))
		}
		matched := operationErr == nil
		if operation.Expectation.Kind != 0 {
			if operation.Every {
				matched, err = everyMatches(jsonShape(observation.Value), operation.ProjectName, expectation)
			} else {
				matched, err = engine.MatchExpectation(jsonShape(observation.Value), expectation)
			}
			if err != nil {
				return observation, false, engine.Fatal(protocol.NewError(protocol.CodeInvalidArgument, err.Error()))
			}
			operationErr = clearedReadError(matched, operationErr)
		}
		return observation, matched, operationErr
	}, nil
}

func everyMatches(actual any, projectName string, expectation engine.Expectation) (bool, error) {
	value := reflect.ValueOf(actual)
	if !value.IsValid() || (value.Kind() != reflect.Array && value.Kind() != reflect.Slice) || value.Len() == 0 {
		return false, nil
	}
	for index := 0; index < value.Len(); index++ {
		item := value.Index(index).Interface()
		if projectName != "" {
			properties, ok := item.(map[string]any)
			if !ok {
				return false, nil
			}
			item = properties[projectName]
		}
		matched, err := engine.MatchExpectation(item, expectation)
		if err != nil || !matched {
			return matched, err
		}
	}
	return true, nil
}

func modifiersFromProtocol(values []string) (engine.Modifier, error) {
	var modifiers engine.Modifier
	for _, value := range values {
		switch value {
		case "Shift":
			modifiers |= engine.ShiftModifier
		case "Control":
			modifiers |= engine.ControlModifier
		case "Alt":
			modifiers |= engine.AltModifier
		case "Meta":
			modifiers |= engine.MetaModifier
		default:
			return 0, protocol.NewError(protocol.CodeInvalidArgument, fmt.Sprintf("unsupported modifier %q", value))
		}
	}
	return modifiers, nil
}

func buttonFromProtocol(value string) (engine.MouseButton, error) {
	switch value {
	case "", "left":
		return engine.LeftButton, nil
	case "right":
		return engine.RightButton, nil
	case "middle":
		return engine.MiddleButton, nil
	default:
		return 0, protocol.NewError(protocol.CodeInvalidArgument, fmt.Sprintf("unsupported mouse button %q", value))
	}
}

// evaluationScript turns an evaluate request into one snippet.  `invoke` is what says whether
// `expression` is a value to evaluate or a function to call: without it the meaning of the
// expression hangs off how many arguments happen to accompany it, so `(a) => a + 1` with no
// arguments evaluates to the function source rather than calling it.  A nil `invoke` is the
// pre-`invoke` client, which is entitled to that inference.
func evaluationScript(expression, argumentsJSON string, invoke *bool) (string, error) {
	arguments, err := evaluationArguments(argumentsJSON)
	if err != nil {
		return "", err
	}
	if invoke != nil && !*invoke {
		if len(arguments) > 0 {
			return "", protocol.NewError(protocol.CodeInvalidArgument, "arguments_json requires invoke: true")
		}
		return expression, nil
	}
	if invoke == nil && len(arguments) == 0 {
		return expression, nil
	}
	encoded, err := json.Marshal(arguments)
	if err != nil {
		return "", protocol.NewError(protocol.CodeInvalidArgument, fmt.Sprintf("arguments_json: %v", err))
	}
	return fmt.Sprintf("(%s)(...%s)", expression, encoded), nil
}

func evaluationArguments(argumentsJSON string) ([]any, error) {
	if strings.TrimSpace(argumentsJSON) == "" {
		return nil, nil
	}
	var arguments []any
	if err := json.Unmarshal([]byte(argumentsJSON), &arguments); err != nil {
		return nil, protocol.NewError(protocol.CodeInvalidArgument, fmt.Sprintf("arguments_json must be a JSON array: %v", err))
	}
	return arguments, nil
}

func (s *engineSession) poll(ctx context.Context, operation protocol.Operation, assertion engine.Assertion) (protocol.Result, error) {
	policy := pollPolicyFromProtocol(operation.Poll)
	policy = withDefaultPollTimeout(policy)
	if operation.Kind == protocol.OperationAssert || operation.Kind == protocol.OperationDOM {
		policy.AttemptTimeout = assertionAttemptTimeout
	}
	result, pollErr := engine.Poll(ctx, policy, assertion)
	converted := pollResult(result, pollErr == nil)
	if pollErr == nil {
		return converted, nil
	}
	// A poll that stopped because retrying was pointless is a failure of a different kind from a
	// poll that ran out of time, and has to reach the client as one - reporting "the browser is
	// gone" as matched:false would render as an assertion timeout and send the reader to the page.
	if engine.IsFatal(pollErr) {
		return protocol.Result{}, engineRPCError(pollErr)
	}
	if errors.Is(pollErr, context.Canceled) && errors.Is(ctx.Err(), context.Canceled) {
		return protocol.Result{}, protocol.NewError(protocol.CodeCancelled, ctx.Err().Error())
	}
	if errors.Is(pollErr, context.DeadlineExceeded) && ctx.Err() != nil {
		return protocol.Result{}, protocol.NewError(protocol.CodeTimeout, ctx.Err().Error())
	}
	diagnosticCtx, cancel := context.WithTimeout(context.Background(), diagnosticsBudget(ctx, time.Now()))
	defer cancel()
	diagnostics, diagnosticErr := s.captureContextDiagnostics(diagnosticCtx, protocol.ContextDiagnosticsOptions{Purpose: "failure", Name: "biloba-failure", Screenshots: true, Outlines: true, MaxBytes: s.maxScreenshotBytes}, "biloba-failure")
	converted.Diagnostics = protocol.Diagnostics{
		Locator: locatorDescription(operation), Expected: expectedDescription(operation),
		DaemonDetail: pollErr.Error(), Context: &diagnostics,
	}
	if len(diagnostics.Tabs) > 0 {
		converted.Diagnostics.DOMOutline = diagnostics.Tabs[0].DOMOutline
		converted.Diagnostics.ScreenshotPath = diagnostics.Tabs[0].ScreenshotPath
	}
	if diagnosticErr != nil {
		converted.Diagnostics.DaemonDetail += "; capture diagnostics: " + diagnosticErr.Error()
	}
	return converted, nil
}

func withDefaultPollTimeout(policy engine.PollPolicy) engine.PollPolicy {
	if policy.Timeout <= 0 {
		policy.Timeout = defaultPollTimeout
	}
	return policy
}

// The client arms its own timer at the very deadline it puts on the request, so capturing
// diagnostics has to fit inside what is left of that deadline: a screenshot that lands late is a
// response nobody is waiting for any more, and the failure the daemon exists to describe arrives
// as a bare "request timed out" with no trajectory, outline, or screenshot at all.  Budget from
// the time actually remaining, reserving enough of it to encode, frame, and write the answer.
const (
	diagnosticsCap     = 2 * time.Second
	diagnosticsReserve = 300 * time.Millisecond
	diagnosticsFloor   = 100 * time.Millisecond
)

func diagnosticsBudget(ctx context.Context, now time.Time) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return diagnosticsCap // nothing to race: an open-ended request gets the fixed budget
	}
	switch budget := deadline.Sub(now) - diagnosticsReserve; {
	case budget > diagnosticsCap:
		return diagnosticsCap
	case budget < diagnosticsFloor:
		// Already out of room.  Try anyway, briefly: a capture that fails fast still leaves the
		// trajectory on the response, and a long shot at an outline beats a certain miss.
		return diagnosticsFloor
	default:
		return budget
	}
}

func (s *engineSession) assertion(assertion protocol.Assertion) (engine.Assertion, error) {
	var selector engine.Selector
	var err error
	if assertion.Kind != protocol.AssertionURL && assertion.Kind != protocol.AssertionEvaluate && assertion.Kind != protocol.AssertionRequest {
		selector, err = selectorFromProtocol(assertion.Locator)
		if err != nil {
			return nil, err
		}
	}
	expectation, err := expectationFromProtocol(assertion.Expectation)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) (engine.Observation, bool, error) {
		var observation engine.Observation
		var readErr error
		switch assertion.Kind {
		case protocol.AssertionVisible:
			observation, readErr = s.session.Visible(ctx, selector)
		case protocol.AssertionExists:
			observation, readErr = s.session.Exists(ctx, selector)
		case protocol.AssertionEnabled:
			observation, readErr = s.session.Enabled(ctx, selector)
		case protocol.AssertionClickable:
			observation, readErr = s.session.Clickable(ctx, selector)
		case protocol.AssertionText:
			observation, readErr = s.session.Text(ctx, selector)
		case protocol.AssertionCount:
			observation, readErr = s.session.Count(ctx, selector)
		case protocol.AssertionAttribute:
			observation, readErr = s.session.Attribute(ctx, selector, assertion.Attribute)
		case protocol.AssertionProperty:
			observation, readErr = s.session.Property(ctx, selector, assertion.Property)
		case protocol.AssertionAllText:
			observation, readErr = s.session.TextForEach(ctx, selector)
		case protocol.AssertionValue:
			observation, readErr = s.session.Value(ctx, selector)
		case protocol.AssertionURL:
			observation, readErr = s.session.URL(ctx)
		case protocol.AssertionEvaluate:
			var value any
			value, readErr = s.session.Evaluate(ctx, assertion.Expression)
			observation = engine.Observation{Value: value}
		case protocol.AssertionRequest:
			requests := s.session.Requests()
			observed := make([]map[string]any, 0, len(requests))
			matched := false
			for _, request := range requests {
				observed = append(observed, map[string]any{"url": request.URL, "method": request.Method})
				urlMatches, matchErr := engine.MatchExpectation(request.URL, expectation)
				if matchErr != nil {
					return engine.Observation{Value: observed}, false, engine.Fatal(protocol.NewError(protocol.CodeInvalidArgument, matchErr.Error()))
				}
				if urlMatches && (assertion.Method == "" || request.Method == assertion.Method) {
					matched = true
				}
			}
			return engine.Observation{Value: observed}, matched, nil
		default:
			// Unreachable from the wire (assertionFromWire rejects unknown kinds first), but if it
			// ever were reached, retrying would turn a rejected request into an assertion timeout.
			return observation, false, engine.Fatal(protocol.NewError(protocol.CodeInvalidArgument, "unsupported assertion"))
		}
		matched, compareErr := engine.MatchExpectation(jsonShape(observation.Value), expectation)
		if compareErr != nil {
			return observation, false, engine.Fatal(protocol.NewError(protocol.CodeInvalidArgument, compareErr.Error()))
		}
		readErr = clearedReadError(matched, readErr)
		return observation, matched, readErr
	}, nil
}

func pollPolicyFromProtocol(policy protocol.PollPolicy) engine.PollPolicy {
	mode := engine.PollEventually
	switch policy.Mode {
	case protocol.PollImmediate:
		mode = engine.PollImmediate
	case protocol.PollConsistently:
		mode = engine.PollConsistently
	}
	return engine.PollPolicy{Timeout: policy.Timeout, Interval: policy.Interval, Mode: mode}
}

func expectationFromProtocol(expectation protocol.Expectation) (engine.Expectation, error) {
	kinds := map[protocol.ExpectationKind]engine.ExpectationKind{
		protocol.ExpectEqual: engine.ExpectEqual, protocol.ExpectContains: engine.ExpectContains,
		protocol.ExpectRegexp: engine.ExpectRegexp, protocol.ExpectPrefix: engine.ExpectPrefix,
		protocol.ExpectSuffix: engine.ExpectSuffix, protocol.ExpectNumber: engine.ExpectNumber,
		protocol.ExpectEmpty: engine.ExpectEmpty, protocol.ExpectAll: engine.ExpectAll,
		protocol.ExpectAny: engine.ExpectAny, protocol.ExpectNot: engine.ExpectNot,
		protocol.ExpectAnything: engine.ExpectAnything,
	}
	kind, ok := kinds[expectation.Kind]
	if !ok {
		return engine.Expectation{}, protocol.NewError(protocol.CodeInvalidArgument, "unsupported expectation")
	}
	converted := engine.Expectation{Kind: kind, Operator: expectation.Operator}
	if expectation.ExpectedJSON != "" {
		if err := json.Unmarshal([]byte(expectation.ExpectedJSON), &converted.Expected); err != nil {
			return engine.Expectation{}, protocol.NewError(protocol.CodeInvalidArgument, fmt.Sprintf("expectedJson is not valid JSON: %v", err))
		}
	}
	for _, child := range expectation.Children {
		convertedChild, err := expectationFromProtocol(child)
		if err != nil {
			return engine.Expectation{}, err
		}
		converted.Children = append(converted.Children, convertedChild)
	}
	return converted, nil
}

func selectorFromProtocol(locator protocol.Locator) (engine.Selector, error) {
	mode := engine.Exact
	if locator.Match == protocol.MatchContains {
		mode = engine.Contains
	}
	var selector engine.Selector
	switch locator.Kind {
	case protocol.LocatorCSS:
		selector = engine.CSS(locator.Value)
	case protocol.LocatorXPath:
		selector = engine.XPath(locator.Value)
	case protocol.LocatorTestID:
		if locator.Attribute != "" {
			selector = engine.TestIDAttribute(locator.Value, locator.Attribute)
		} else {
			selector = engine.TestID(locator.Value)
		}
	case protocol.LocatorText:
		selector = engine.Text(locator.Value, mode)
	case protocol.LocatorRole:
		selector = engine.Role(locator.Role, locator.Name, mode)
	case protocol.LocatorLabel:
		selector = engine.Label(locator.Value, mode)
	case protocol.LocatorPlaceholder:
		selector = engine.Placeholder(locator.Value, mode)
	case protocol.LocatorAltText:
		selector = engine.AltText(locator.Value, mode)
	case protocol.LocatorTitle:
		selector = engine.Title(locator.Value, mode)
	case protocol.LocatorAnd, protocol.LocatorOr:
		first, err := selectorFromProtocol(locator.Operands[0])
		if err != nil {
			return engine.Selector{}, err
		}
		selector = first
		for _, operand := range locator.Operands[1:] {
			converted, convertErr := selectorFromProtocol(operand)
			if convertErr != nil {
				return engine.Selector{}, convertErr
			}
			if locator.Kind == protocol.LocatorAnd {
				selector = selector.And(converted)
			} else {
				selector = selector.Or(converted)
			}
		}
	default:
		return engine.Selector{}, protocol.NewError(protocol.CodeInvalidArgument, "unsupported locator")
	}
	if locator.Within != nil {
		within, err := selectorFromProtocol(*locator.Within)
		if err != nil {
			return engine.Selector{}, err
		}
		selector = selector.Within(within)
	}
	for _, filter := range locator.Filters {
		switch filter.Kind {
		case protocol.LocatorFilterContainsText:
			if filter.Negate {
				selector = selector.NotContainingText(filter.Value)
			} else {
				selector = selector.ContainingText(filter.Value)
			}
		case protocol.LocatorFilterContains, protocol.LocatorFilterWithin:
			converted, err := selectorFromProtocol(*filter.Selector)
			if err != nil {
				return engine.Selector{}, err
			}
			switch {
			case filter.Kind == protocol.LocatorFilterWithin && filter.Negate:
				selector = selector.NotWithin(converted)
			case filter.Negate:
				selector = selector.NotContaining(converted)
			default:
				selector = selector.Containing(converted)
			}
		}
	}
	if locator.LevelSet {
		selector = selector.Level(locator.Level)
	}
	for _, state := range locator.States {
		switch state {
		case "checked":
			selector = selector.Checked()
		case "disabled":
			selector = selector.Disabled()
		case "expanded":
			selector = selector.Expanded()
		case "pressed":
			selector = selector.Pressed()
		case "selected":
			selector = selector.Selected()
		default:
			return engine.Selector{}, protocol.NewError(protocol.CodeInvalidArgument, "unsupported locator state")
		}
	}
	if locator.NthSet {
		selector = selector.Nth(locator.Nth)
	}
	return selector, nil
}

func oneAttempt(started time.Time, err error) (protocol.Result, error) {
	if err != nil {
		return protocol.Result{}, engineRPCError(err)
	}
	return protocol.Result{Matched: true, Attempts: 1, StartedAt: started, Elapsed: time.Since(started)}, nil
}

func observedResult(started time.Time, value any) protocol.Result {
	return protocol.Result{Matched: true, ObservedJSON: marshalJSON(value), Attempts: 1, StartedAt: started, Elapsed: time.Since(started)}
}

func pollResult(result engine.PollResult, matched bool) protocol.Result {
	trajectory := make([]protocol.Observation, len(result.Attempts))
	for i, attempt := range result.Attempts {
		trajectory[i] = protocol.Observation{Attempt: uint32(attempt.Number), Elapsed: attempt.StartedAt.Sub(result.StartedAt), ObservedJSON: marshalJSON(attempt.Observation.Value), RetryReason: attempt.Error}
	}
	return protocol.Result{Matched: matched, ObservedJSON: marshalJSON(result.Final.Value), Attempts: uint32(result.AttemptCount), Trajectory: trajectory, StartedAt: result.StartedAt, Elapsed: result.Duration}
}

func stringMatches(value any, expected string, mode protocol.MatchMode) bool {
	observed, ok := value.(string)
	if !ok {
		return false
	}
	if mode == protocol.MatchContains {
		return strings.Contains(observed, expected)
	}
	return observed == expected
}

func numericEqual(value any, expected int64) bool {
	switch value := value.(type) {
	case int:
		return int64(value) == expected
	case int64:
		return value == expected
	case float64:
		return value == float64(expected)
	default:
		return false
	}
}

// jsonEqual compares an observation against the caller's expected value.  A malformed expectation
// is fatal rather than simply unequal: no amount of polling turns invalid JSON into a value, and
// reporting it as a mismatch would spend the whole budget and then blame the page - quoting what
// was observed while saying nothing about the expectation that never parsed.  This is the same
// judgement capture.go makes with gomega.StopTrying for a decode into the wrong pointer type.
func jsonEqual(observed any, expectedJSON string) (bool, error) {
	var expected any
	if err := json.Unmarshal([]byte(expectedJSON), &expected); err != nil {
		return false, engine.Fatal(protocol.NewError(protocol.CodeInvalidArgument,
			fmt.Sprintf("expectedJson is not valid JSON: %v", err)))
	}
	return reflect.DeepEqual(observed, expected), nil
}

func marshalJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%q", fmt.Sprint(value))
	}
	return string(encoded)
}

func locatorDescription(operation protocol.Operation) string {
	locator := operation.Locator
	if operation.Kind == protocol.OperationAssert {
		locator = operation.Assertion.Locator
	} else if operation.Kind == protocol.OperationDOM {
		locator = operation.DOM.Locator
	}
	selector, err := selectorFromProtocol(locator)
	if err != nil {
		return ""
	}
	return selector.Description()
}

func expectedDescription(operation protocol.Operation) string {
	if operation.Kind == protocol.OperationDOM {
		if operation.DOM.Expectation.Kind != 0 {
			return expectationDescription(operation.DOM.Expectation)
		}
		return "operation to succeed"
	}
	if operation.Kind != protocol.OperationAssert {
		return "operation to succeed"
	}
	assertion := operation.Assertion
	if semantic := booleanAssertionDescription(assertion); semantic != "" {
		return semantic
	}
	if assertion.Expectation.Kind != 0 {
		return expectationDescription(assertion.Expectation)
	}
	switch assertion.Kind {
	case protocol.AssertionVisible:
		return "visible"
	case protocol.AssertionCount:
		return fmt.Sprint(assertion.ExpectedCount)
	case protocol.AssertionValue, protocol.AssertionEvaluate:
		return assertion.ExpectedJSON
	default:
		return assertion.ExpectedString
	}
}

func booleanAssertionDescription(assertion protocol.Assertion) string {
	var positive string
	switch assertion.Kind {
	case protocol.AssertionVisible:
		positive = "visible"
	case protocol.AssertionExists:
		positive = "exist"
	case protocol.AssertionEnabled:
		positive = "enabled"
	case protocol.AssertionClickable:
		positive = "clickable"
	default:
		return ""
	}
	if assertion.Expectation.Kind != protocol.ExpectEqual {
		return ""
	}
	var expected bool
	if json.Unmarshal([]byte(assertion.Expectation.ExpectedJSON), &expected) != nil {
		return ""
	}
	if expected {
		return positive
	}
	if positive == "exist" {
		return "not exist"
	}
	return "not " + positive
}

func expectationDescription(expectation protocol.Expectation) string {
	var expected any
	if expectation.ExpectedJSON != "" && json.Unmarshal([]byte(expectation.ExpectedJSON), &expected) == nil {
		if expectation.Kind == protocol.ExpectEqual {
			return fmt.Sprint(expected)
		}
	}
	switch expectation.Kind {
	case protocol.ExpectContains:
		return fmt.Sprintf("contain %q", expected)
	case protocol.ExpectRegexp:
		return fmt.Sprintf("match %q", expected)
	case protocol.ExpectPrefix:
		return fmt.Sprintf("start with %q", expected)
	case protocol.ExpectSuffix:
		return fmt.Sprintf("end with %q", expected)
	case protocol.ExpectNumber:
		return fmt.Sprintf("%s %v", expectation.Operator, expected)
	case protocol.ExpectEmpty:
		return "empty"
	case protocol.ExpectAnything:
		return "any value"
	case protocol.ExpectAll, protocol.ExpectAny:
		parts := make([]string, len(expectation.Children))
		for i, child := range expectation.Children {
			parts[i] = expectationDescription(child)
		}
		joiner := " and "
		if expectation.Kind == protocol.ExpectAny {
			joiner = " or "
		}
		return strings.Join(parts, joiner)
	case protocol.ExpectNot:
		if len(expectation.Children) == 1 {
			return "not " + expectationDescription(expectation.Children[0])
		}
	}
	return expectation.ExpectedJSON
}

// engineProtocolCodes is the whole engine-to-protocol error vocabulary, in one place.  A code that
// is missing here reaches the client as a generic DRIVER_ERROR - which reads as "the daemon broke"
// for a failure the page caused, and buries the one thing the client could act on (a page-level
// JavaScript error is JAVASCRIPT_ERROR; a navigation that never landed leaves the target not
// ready).  The two codes with no honest counterpart are listed with the reason rather than left
// out, so a code added to the engine shows up as a missing key - see the exhaustiveness spec in
// main_test.go - instead of silently inheriting a plausible-looking default.
var engineProtocolCodes = map[engine.ErrorCode]protocol.ErrorCode{
	engine.CodeInvalidSelector: protocol.CodeInvalidArgument,
	engine.CodeInvalidArgument: protocol.CodeInvalidArgument,
	engine.CodeSessionClosed:   protocol.CodeTargetNotFound,
	engine.CodeNotFound:        protocol.CodeTargetNotFound,
	// The target was found and refused the operation - a click on a hidden element.  That is a page
	// state, not a driver fault, and it is the one bucket where a retry might succeed.
	engine.CodeActionFailed: protocol.CodeTargetNotReady,
	// The handler ran and answered no.  Same bucket as a refused action: the page could still change
	// its mind, so a retry is meaningful.
	engine.CodeConditionNotMet: protocol.CodeTargetNotReady,
	// A navigation that landed on a status the caller did not ask for.  Deliberately not
	// TARGET_NOT_READY: the page loaded fine, so waiting will never change the answer.
	engine.CodeNavigation:    protocol.CodeNavigation,
	engine.CodeJavaScript:    protocol.CodeJavaScript,
	engine.CodeInvalidScript: protocol.CodeJavaScript,
	engine.CodeBrowserGone:   protocol.CodeBrowserGone,
	engine.CodePageCrashed:   protocol.CodePageCrashed,
	engine.CodeCanceled:      protocol.CodeCancelled,
	engine.CodeTimeout:       protocol.CodeTimeout,
	engine.CodeDeadline:      protocol.CodeTimeout,
	// No counterpart, deliberately.  Both are the daemon failing to bring its own Chrome up, before
	// there is a session to report against - BROWSER_GONE means a live Chrome died underneath a
	// worker, which is a different thing to tell a client.
	engine.CodeBrowserStart: protocol.CodeDriver,
	engine.CodeIO:           protocol.CodeDriver,
}

func engineRPCError(err error) error {
	if err == nil {
		return nil
	}
	// A ProtocolError that travelled out through the engine (an engine.Fatal wrapping one, say)
	// already knows its own code - do not flatten it to DRIVER_ERROR on the way past.
	var protocolErr *protocol.ProtocolError
	if errors.As(err, &protocolErr) {
		return protocolErr
	}
	var engineErr *engine.Error
	if !errors.As(err, &engineErr) {
		return protocol.NewError(protocol.CodeDriver, err.Error())
	}
	code, ok := engineProtocolCodes[engineErr.Code]
	if !ok {
		code = protocol.CodeDriver
	}
	return protocol.NewError(code, engineErr.Error())
}

// jsonShape renders an observation the way the client sees it, so an expectation decoded from the
// wire can be compared against it.  The engine answers in Go types - engine.DocumentOrder is a named
// string, engine.Box and its geometry siblings are structs - while the expectation arrives as
// decoded JSON.  reflect.DeepEqual is type-sensitive, so without this an EQUAL fed the exact value
// the matching read just returned is false forever: the assertion cannot fail loudly, it can only
// time out.
func jsonShape(value any) any {
	switch value.(type) {
	case nil, bool, string, float64, int, int64, []any, map[string]any:
		return value
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return value
	}
	return decoded
}

// clearedReadError decides whether a satisfied expectation may swallow the error the read reported.
// Only CodeConditionNotMet may be swallowed - the handler ran against a real element and answered
// "no", which for a negated matcher is the answer rather than a failure.  Everything else survives,
// above all CodeNotFound: biloba.js raises a selector that matched nothing as an error precisely so
// that ShouldNot(<matcher>) cannot pass vacuously against an element that was never there (see the
// poll() comment in biloba.js).  Swallowing it makes expectNotVisible("#missing") pass instantly and
// makes a poll for a not-yet-rendered value answer null on its first attempt instead of waiting.
func clearedReadError(matched bool, err error) error {
	if !matched || err == nil || engine.IsFatal(err) {
		return err
	}
	var engineErr *engine.Error
	if errors.As(err, &engineErr) && engineErr.Code == engine.CodeConditionNotMet {
		return nil
	}
	return err
}

// eventfulAssertionResult shapes a server-side poll the way the client's AssertionResult expects.
// The trajectory is the poll's own attempts: hard-coding it empty made expectNetworkIdle report a
// typed result whose diagnostic half was a fiction, so "the network never went idle" said nothing
// about what the in-flight count had been doing.  rpcRequestCount stays 1 because that is the truth
// of a server-side poll - one request owns the whole retry loop.
func eventfulAssertionResult(result engine.PollResult) map[string]any {
	trajectory := make([]any, len(result.Attempts))
	for index, attempt := range result.Attempts {
		entry := map[string]any{
			"attempt":   attempt.Number,
			"elapsedMs": attempt.StartedAt.Sub(result.StartedAt).Milliseconds(),
			"observed":  attempt.Observation.Value,
		}
		if attempt.Error != "" {
			entry["retryReason"] = attempt.Error
		}
		trajectory[index] = entry
	}
	return map[string]any{
		"observed": result.Final.Value, "attemptCount": result.AttemptCount, "trajectory": trajectory,
		"rpcRequestCount": 1, "rpcResponseCount": 1, "elapsedMs": result.Duration.Milliseconds(),
	}
}
