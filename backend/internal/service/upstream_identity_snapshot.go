package service

import (
	"context"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
)

// upstreamIdentitySnapshot is the immutable subset of outbound headers that
// is useful in the usage log. Pointers intentionally distinguish an observed
// request with an empty header from a historical row where no outbound request
// was available.
type upstreamIdentitySnapshot struct {
	turnState  *string
	originator *string
	userAgent  *string
	version    *string
}

type upstreamIdentitySnapshotKey struct{}
type upstreamIdentityCaptureKey struct{}

type upstreamIdentityCapture struct {
	mu    sync.RWMutex
	value *upstreamIdentitySnapshot
}

func withUpstreamIdentityCapture(ctx context.Context) (context.Context, *upstreamIdentityCapture) {
	if ctx == nil {
		ctx = context.Background()
	}
	if capture, ok := ctx.Value(upstreamIdentityCaptureKey{}).(*upstreamIdentityCapture); ok && capture != nil {
		return ctx, capture
	}
	capture := &upstreamIdentityCapture{}
	return context.WithValue(ctx, upstreamIdentityCaptureKey{}, capture), capture
}

func (c *upstreamIdentityCapture) store(snapshot *upstreamIdentitySnapshot) {
	if c == nil || snapshot == nil {
		return
	}
	c.mu.Lock()
	c.value = snapshot
	c.mu.Unlock()
}

func (c *upstreamIdentityCapture) load() *upstreamIdentitySnapshot {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.value
}

func captureUpstreamIdentity(ctx context.Context, snapshot *upstreamIdentitySnapshot) {
	if ctx == nil || snapshot == nil {
		return
	}
	if capture, ok := ctx.Value(upstreamIdentityCaptureKey{}).(*upstreamIdentityCapture); ok {
		capture.store(snapshot)
	}
}

func stringPointer(value string) *string {
	copy := value
	return &copy
}

func snapshotUpstreamIdentity(headers http.Header) *upstreamIdentitySnapshot {
	return &upstreamIdentitySnapshot{
		turnState:  stringPointer(headers.Get(openAICodexTurnStateHeader)),
		originator: stringPointer(headers.Get("originator")),
		userAgent:  stringPointer(headers.Get("user-agent")),
		version:    stringPointer(headers.Get("version")),
	}
}

// snapshotUpstreamRequestIdentity attaches a copy of the selected outbound
// headers to the request context before dispatch. The copy remains stable even
// when a retry or response handler later mutates the request header map.
func snapshotUpstreamRequestIdentity(request *http.Request) *http.Request {
	if request == nil {
		return nil
	}
	snapshot := snapshotUpstreamIdentity(request.Header)
	captureUpstreamIdentity(request.Context(), snapshot)
	return request.WithContext(context.WithValue(request.Context(), upstreamIdentitySnapshotKey{}, snapshot))
}

// snapshotDispatchedUpstreamRequest runs after the transport returns because
// the shared transport may apply final host-specific headers in place. It also
// binds the immutable request snapshot to the response for error/cyber paths.
func snapshotDispatchedUpstreamRequest(request *http.Request, response *http.Response) *http.Request {
	dispatchedRequest := request
	if response != nil && response.Request != nil {
		dispatchedRequest = response.Request
	}
	if dispatchedRequest == nil {
		return nil
	}

	snapshot := snapshotUpstreamIdentity(dispatchedRequest.Header)
	// A transport may clone the request for redirects or provider-specific
	// fallback. Preserve the caller's shared capture even if that clone replaced
	// its context, then bind the immutable snapshot to the final request too.
	if request != nil {
		captureUpstreamIdentity(request.Context(), snapshot)
	}
	if request == nil || dispatchedRequest != request {
		captureUpstreamIdentity(dispatchedRequest.Context(), snapshot)
	}
	dispatchedRequest = dispatchedRequest.WithContext(context.WithValue(dispatchedRequest.Context(), upstreamIdentitySnapshotKey{}, snapshot))
	if response != nil {
		response.Request = dispatchedRequest
	}
	return dispatchedRequest
}

func upstreamIdentityFromRequest(request *http.Request) *upstreamIdentitySnapshot {
	if request == nil {
		return nil
	}
	if snapshot, ok := request.Context().Value(upstreamIdentitySnapshotKey{}).(*upstreamIdentitySnapshot); ok {
		return snapshot
	}
	return snapshotUpstreamIdentity(request.Header)
}

func upstreamIdentityFromResponse(response *http.Response) *upstreamIdentitySnapshot {
	if response == nil {
		return nil
	}
	return upstreamIdentityFromRequest(response.Request)
}

func (snapshot *upstreamIdentitySnapshot) applyToOpenAIResult(result *OpenAIForwardResult) {
	if snapshot == nil || result == nil {
		return
	}
	result.UpstreamTurnState = snapshot.turnState
	result.UpstreamOriginator = snapshot.originator
	result.UpstreamUserAgent = snapshot.userAgent
	result.UpstreamVersion = snapshot.version
}

func (snapshot *upstreamIdentitySnapshot) applyToOpenAIResultIfUnset(result *OpenAIForwardResult) {
	if result == nil || result.UpstreamTurnState != nil || result.UpstreamOriginator != nil ||
		result.UpstreamUserAgent != nil || result.UpstreamVersion != nil {
		return
	}
	snapshot.applyToOpenAIResult(result)
}

func (snapshot *upstreamIdentitySnapshot) applyToForwardResult(result *ForwardResult) {
	if snapshot == nil || result == nil {
		return
	}
	result.UpstreamTurnState = snapshot.turnState
	result.UpstreamOriginator = snapshot.originator
	result.UpstreamUserAgent = snapshot.userAgent
	result.UpstreamVersion = snapshot.version
}

func (snapshot *upstreamIdentitySnapshot) applyToForwardResultIfUnset(result *ForwardResult) {
	if result == nil || result.UpstreamTurnState != nil || result.UpstreamOriginator != nil ||
		result.UpstreamUserAgent != nil || result.UpstreamVersion != nil {
		return
	}
	snapshot.applyToForwardResult(result)
}

func (snapshot *upstreamIdentitySnapshot) applyToCyberPolicyMark(mark *CyberPolicyMark) {
	if snapshot == nil || mark == nil {
		return
	}
	mark.UpstreamTurnState = snapshot.turnState
	mark.UpstreamOriginator = snapshot.originator
	mark.UpstreamUserAgent = snapshot.userAgent
	mark.UpstreamVersion = snapshot.version
}

func applyCapturedUpstreamIdentityToOpenAIResult(capture *upstreamIdentityCapture, result *OpenAIForwardResult, c *gin.Context) {
	if capture == nil {
		return
	}
	snapshot := capture.load()
	snapshot.applyToOpenAIResultIfUnset(result)
	snapshot.applyToCyberPolicyMark(GetOpsCyberPolicy(c))
}

func applyCapturedUpstreamIdentityToForwardResult(capture *upstreamIdentityCapture, result *ForwardResult) {
	if capture == nil {
		return
	}
	capture.load().applyToForwardResultIfUnset(result)
}
