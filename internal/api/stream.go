package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/orxest/orxest/internal/domain"
)

// writeSSE writes one Server-Sent Event.
func writeSSE(w http.ResponseWriter, flusher http.Flusher, event string, id string, payload any) {
	if event != "" {
		fmt.Fprintf(w, "event: %s\n", event)
	}
	if id != "" {
		fmt.Fprintf(w, "id: %s\n", id)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		data = []byte(`{"error":"cannot encode event"}`)
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

func prepareSSE(w http.ResponseWriter, r *http.Request) (http.Flusher, bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	header := w.Header()
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	header.Set("Cache-Control", "no-cache, no-transform")
	header.Set("Connection", "keep-alive")
	// Disable proxy buffering so events arrive promptly.
	header.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	return flusher, true
}

// handleProjectStream streams both orchestration events and execution output of
// a project (spec §32).
func (s *Server) handleProjectStream(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	projectID := pathValue(r, "id")
	if _, err := s.svc.Projects.Get(ctx, projectID); err != nil {
		s.fail(w, r, err)
		return
	}
	flusher, ok := prepareSSE(w, r)
	if !ok {
		s.fail(w, r, domain.Unavailablef("streaming is not supported by this connection"))
		return
	}
	busSub := s.svc.Deps.Bus.Subscribe(projectID)
	defer busSub.Close()
	streamCh, cancelStream := s.svc.Deps.Stream.SubscribeProject(projectID)
	defer cancelStream()

	writeSSE(w, flusher, "stream.ready", "", map[string]any{
		"project_id":  projectID,
		"server_time": time.Now().UTC(),
	})
	// Replay the most recent activity so a fresh browser view is not empty.
	if recent, err := s.svc.Events.ByProject(ctx, projectID, 25, 0); err == nil {
		for i := len(recent) - 1; i >= 0; i-- {
			writeSSE(w, flusher, recent[i].Type, recent[i].ID, recent[i])
		}
	}
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	s.log.InfoContext(ctx, "sse stream opened", "project_id", projectID)
	defer s.log.InfoContext(ctx, "sse stream closed", "project_id", projectID)

	for {
		select {
		case <-ctx.Done():
			return
		case evt, open := <-busSub.Events():
			if !open {
				return
			}
			writeSSE(w, flusher, evt.Type, evt.ID, evt)
		case evt, open := <-streamCh:
			if !open {
				return
			}
			writeSSE(w, flusher, "execution."+evt.Type, evt.ID, evt)
		case <-heartbeat.C:
			fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		}
	}
}

// handleExecutionStream streams the live output of one execution.
func (s *Server) handleExecutionStream(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	executionID := pathValue(r, "id")
	exec, err := s.svc.Executions.Get(ctx, executionID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	flusher, ok := prepareSSE(w, r)
	if !ok {
		s.fail(w, r, domain.Unavailablef("streaming is not supported by this connection"))
		return
	}
	busSub := s.svc.Deps.Bus.Subscribe("")
	defer busSub.Close()
	streamCh, cancelStream := s.svc.Deps.Stream.SubscribeExecution(executionID)
	defer cancelStream()

	writeSSE(w, flusher, "stream.ready", "", map[string]any{
		"execution_id": executionID,
		"status":       exec.Status,
	})
	// Replay the persisted log, then continue live.
	afterSeq := 0
	if events, err := s.svc.Events.ExecutionEvents(ctx, executionID, 0, 2000); err == nil {
		for _, evt := range events {
			writeSSE(w, flusher, "execution."+evt.Type, evt.ID, evt)
			afterSeq = evt.Seq
		}
	}
	_ = afterSeq
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case evt, open := <-busSub.Events():
			if !open {
				return
			}
			if evt.ExecutionID == executionID {
				writeSSE(w, flusher, evt.Type, evt.ID, evt)
			}
		case evt, open := <-streamCh:
			if !open {
				return
			}
			writeSSE(w, flusher, "execution."+evt.Type, evt.ID, evt)
		case <-heartbeat.C:
			fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		}
	}
}
