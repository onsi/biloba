package protocol

import (
	"context"
	"encoding/json"
)

const MaxDecodedBodySize int64 = 16 << 20

type EventfulOperationKind string

const (
	EventfulRegisterDialogHandler  EventfulOperationKind = "REGISTER_DIALOG_HANDLER"
	EventfulRemoveDialogHandler    EventfulOperationKind = "REMOVE_DIALOG_HANDLER"
	EventfulDialogs                EventfulOperationKind = "DIALOGS"
	EventfulWarnings               EventfulOperationKind = "WARNINGS"
	EventfulDownloads              EventfulOperationKind = "DOWNLOADS"
	EventfulWaitForDownload        EventfulOperationKind = "WAIT_FOR_DOWNLOAD"
	EventfulDownloadContent        EventfulOperationKind = "DOWNLOAD_CONTENT"
	EventfulCancelDownload         EventfulOperationKind = "CANCEL_DOWNLOAD"
	EventfulRequests               EventfulOperationKind = "REQUESTS"
	EventfulWaitForRequest         EventfulOperationKind = "WAIT_FOR_REQUEST"
	EventfulResponses              EventfulOperationKind = "RESPONSES"
	EventfulWaitForNetworkIdle     EventfulOperationKind = "WAIT_FOR_NETWORK_IDLE"
	EventfulRegisterNetworkHandler EventfulOperationKind = "REGISTER_NETWORK_HANDLER"
	EventfulRemoveNetworkHandler   EventfulOperationKind = "REMOVE_NETWORK_HANDLER"
	EventfulNetworkHandlerStats    EventfulOperationKind = "NETWORK_HANDLER_STATS"
	EventfulNetworkShadows         EventfulOperationKind = "NETWORK_SHADOWS"
	EventfulHoldResponse           EventfulOperationKind = "HOLD_RESPONSE"
	EventfulAwaitResponseHold      EventfulOperationKind = "AWAIT_RESPONSE_HOLD"
	EventfulReleaseResponseHold    EventfulOperationKind = "RELEASE_RESPONSE_HOLD"
	EventfulReleaseHeldResponse    EventfulOperationKind = "RELEASE_HELD_RESPONSE"
	EventfulReleaseNextResponse    EventfulOperationKind = "RELEASE_NEXT_RESPONSE"
	EventfulResponseHoldStats      EventfulOperationKind = "RESPONSE_HOLD_STATS"
	EventfulSetNetworkState        EventfulOperationKind = "SET_NETWORK_STATE"
	EventfulNetworkState           EventfulOperationKind = "NETWORK_STATE"
	EventfulSetCacheEnabled        EventfulOperationKind = "SET_CACHE_ENABLED"
)

type EventfulOperation struct {
	Kind                                                             EventfulOperationKind
	ID, ResponseID, DialogType                                       string
	Message, URL, Method, ResourceType, Filename, State, ContentText *Expectation
	ContentBase64                                                    *string
	Accept                                                           bool
	PromptText                                                       *string
	Limit                                                            int
	MaxBodyBytes                                                     int64
	Callsite                                                         string
	Action                                                           string
	Override                                                         WireNetworkOverride
	CallbackID                                                       string
	TransformTimeoutMS                                               int64
	Network                                                          WireNetworkState
	CacheEnabled                                                     bool
	Poll                                                             PollPolicy
	InvokeCallback                                                   func(context.Context, CallbackInvocation) (WireNetworkOverride, error)
}

type EventfulSession interface {
	Session
	ExecuteEventful(context.Context, EventfulOperation) (any, error)
}

type WireEventfulOperation struct {
	Kind               string               `json:"kind"`
	ID                 string               `json:"id,omitempty"`
	ResponseID         string               `json:"responseId,omitempty"`
	DialogType         string               `json:"dialogType,omitempty"`
	Message            *WireExpectation     `json:"message,omitempty"`
	URL                *WireExpectation     `json:"url,omitempty"`
	Method             *WireExpectation     `json:"method,omitempty"`
	ResourceType       *WireExpectation     `json:"resourceType,omitempty"`
	Filename           *WireExpectation     `json:"filename,omitempty"`
	State              *WireExpectation     `json:"state,omitempty"`
	ContentText        *WireExpectation     `json:"contentText,omitempty"`
	ContentBase64      *string              `json:"contentBase64,omitempty"`
	Accept             bool                 `json:"accept,omitempty"`
	PromptText         *string              `json:"promptText,omitempty"`
	Limit              int                  `json:"limit,omitempty"`
	MaxBodyBytes       int64                `json:"maxBodyBytes,omitempty"`
	Callsite           string               `json:"callsite,omitempty"`
	Action             string               `json:"action,omitempty"`
	Override           *WireNetworkOverride `json:"override,omitempty"`
	CallbackID         string               `json:"callbackId,omitempty"`
	TransformTimeoutMS int64                `json:"transformTimeoutMs,omitempty"`
	Network            *WireNetworkState    `json:"network,omitempty"`
	CacheEnabled       *bool                `json:"cacheEnabled,omitempty"`
}

type WireNetworkOverride struct {
	URL        *string           `json:"url,omitempty"`
	Method     *string           `json:"method,omitempty"`
	Status     *int              `json:"status,omitempty"`
	Headers    []WireHeaderEntry `json:"headers,omitempty"`
	BodyBase64 *string           `json:"bodyBase64,omitempty"`
}

type WireHeaderEntry struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type WireNetworkState struct {
	Offline            bool    `json:"offline,omitempty"`
	LatencyMS          int64   `json:"latencyMs,omitempty"`
	DownloadThroughput float64 `json:"downloadThroughput,omitempty"`
	UploadThroughput   float64 `json:"uploadThroughput,omitempty"`
	ConnectionType     string  `json:"connectionType,omitempty"`
}

type EventfulRequest struct {
	SessionID string                 `json:"sessionId"`
	Operation *WireEventfulOperation `json:"operation"`
	Poll      PollOptions            `json:"poll,omitempty"`
}

type CallbackInvocation struct {
	CallbackID string
	Payload    any
}

type CallbackResultRequest struct {
	InvocationID string          `json:"invocationId"`
	Result       json.RawMessage `json:"result,omitempty"`
	Error        string          `json:"error,omitempty"`
}

type EventFrame struct {
	Event        string `json:"event"`
	InvocationID string `json:"invocationId,omitempty"`
	CallbackID   string `json:"callbackId,omitempty"`
	Payload      any    `json:"payload,omitempty"`
}

type CallbackInvoker interface {
	Invoke(context.Context, CallbackInvocation) (WireNetworkOverride, error)
}
