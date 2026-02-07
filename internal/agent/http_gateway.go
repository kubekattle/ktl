package agent

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	apiv1 "github.com/example/ktl/pkg/api/ktl/api/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const httpAuthCookieName = "ktl_token"

func newHTTPGateway(token string, mirror *MirrorServer) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/auth/cookie", func(w http.ResponseWriter, r *http.Request) {
		if !allowCORS(w, r) {
			return
		}
		if r.Method != http.MethodPost && r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		switch r.Method {
		case http.MethodPost:
			if !requireHTTPAuth(w, r, token) {
				return
			}
			// Store the token in an HttpOnly cookie so native EventSource can authenticate
			// without custom header support.
			expected := strings.TrimSpace(token)
			if expected != "" {
				http.SetCookie(w, &http.Cookie{
					Name:     httpAuthCookieName,
					Value:    expected,
					Path:     "/",
					HttpOnly: true,
					Secure:   r.TLS != nil,
					SameSite: http.SameSiteLaxMode,
				})
			}
			w.WriteHeader(http.StatusNoContent)
			return
		case http.MethodDelete:
			// Allow logout even if the cookie is already invalid.
			http.SetCookie(w, &http.Cookie{
				Name:     httpAuthCookieName,
				Value:    "",
				Path:     "/",
				HttpOnly: true,
				Secure:   r.TLS != nil,
				SameSite: http.SameSiteLaxMode,
				MaxAge:   -1,
			})
			w.WriteHeader(http.StatusNoContent)
			return
		}
	})

	mux.HandleFunc("/api/v1/mirror/sessions", func(w http.ResponseWriter, r *http.Request) {
		if !allowCORS(w, r) {
			return
		}
		if !requireHTTPAuth(w, r, token) {
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		limit := 200
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 {
				limit = n
			}
		}
		req := &apiv1.MirrorListSessionsRequest{Limit: int32(limit)}
		q := r.URL.Query()

		var meta apiv1.MirrorSessionMeta
		metaSet := false
		if v := strings.TrimSpace(q.Get("command")); v != "" {
			meta.Command = v
			metaSet = true
		}
		if v := strings.TrimSpace(q.Get("requester")); v != "" {
			meta.Requester = v
			metaSet = true
		}
		if v := strings.TrimSpace(q.Get("cluster")); v != "" {
			meta.Cluster = v
			metaSet = true
		}
		if v := strings.TrimSpace(q.Get("kube_context")); v != "" {
			meta.KubeContext = v
			metaSet = true
		}
		if v := strings.TrimSpace(q.Get("namespace")); v != "" {
			meta.Namespace = v
			metaSet = true
		}
		if v := strings.TrimSpace(q.Get("release")); v != "" {
			meta.Release = v
			metaSet = true
		}
		if v := strings.TrimSpace(q.Get("chart")); v != "" {
			meta.Chart = v
			metaSet = true
		}
		if metaSet {
			req.Meta = &meta
		}

		if raw := strings.TrimSpace(q.Get("state")); raw != "" {
			switch strings.ToLower(raw) {
			case "running":
				req.State = apiv1.MirrorSessionState_MIRROR_SESSION_STATE_RUNNING
			case "done":
				req.State = apiv1.MirrorSessionState_MIRROR_SESSION_STATE_DONE
			case "error":
				req.State = apiv1.MirrorSessionState_MIRROR_SESSION_STATE_ERROR
			}
		}
		if raw := strings.TrimSpace(firstQuery(q, "since_last_seen_unix_nano", "since_unix_nano", "since")); raw != "" {
			if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
				req.SinceLastSeenUnixNano = n
			}
		}
		if raw := strings.TrimSpace(firstQuery(q, "until_last_seen_unix_nano", "until_unix_nano", "until")); raw != "" {
			if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
				req.UntilLastSeenUnixNano = n
			}
		}

		tags := map[string]string{}
		for _, raw := range q["tag"] {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			k, v, ok := strings.Cut(raw, "=")
			k = strings.TrimSpace(k)
			v = strings.TrimSpace(v)
			if !ok || k == "" {
				continue
			}
			tags[k] = v
		}
		for k, vals := range q {
			if !strings.HasPrefix(k, "tag.") {
				continue
			}
			key := strings.TrimSpace(strings.TrimPrefix(k, "tag."))
			if key == "" {
				continue
			}
			tags[key] = strings.TrimSpace(firstNonEmpty(vals))
		}
		if len(tags) > 0 {
			req.Tags = tags
		}

		resp, err := mirror.ListSessions(r.Context(), req)
		if err != nil {
			writeHTTPError(w, err)
			return
		}
		writeProtoJSON(w, resp)
	})

	mux.HandleFunc("/api/v1/mirror/sessions/", func(w http.ResponseWriter, r *http.Request) {
		if !allowCORS(w, r) {
			return
		}
		if !requireHTTPAuth(w, r, token) {
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/api/v1/mirror/sessions/")
		rest = strings.Trim(rest, "/")
		if rest == "" {
			http.NotFound(w, r)
			return
		}
		parts := strings.Split(rest, "/")
		sessionID := strings.TrimSpace(parts[0])
		if sessionID == "" {
			http.NotFound(w, r)
			return
		}
		if len(parts) == 1 {
			if r.Method != http.MethodGet {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			sess, err := mirror.GetSession(r.Context(), &apiv1.MirrorGetSessionRequest{SessionId: sessionID})
			if err != nil {
				writeHTTPError(w, err)
				return
			}
			writeProtoJSON(w, sess)
			return
		}
		if len(parts) == 2 && parts[1] == "export" {
			if r.Method != http.MethodGet {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			fromSeq := uint64(1)
			if raw := strings.TrimSpace(r.URL.Query().Get("from_sequence")); raw != "" {
				if n, err := strconv.ParseUint(raw, 10, 64); err == nil && n > 0 {
					fromSeq = n
				}
			}
			w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")

			mo := protojson.MarshalOptions{UseProtoNames: true}
			flushEvery := time.Now().UTC().Add(250 * time.Millisecond)
			flusher, _ := w.(http.Flusher)
			writeLine := func(frame *apiv1.MirrorFrame) error {
				if frame == nil {
					return nil
				}
				raw, err := mo.Marshal(frame)
				if err != nil {
					return err
				}
				if _, err := w.Write(append(raw, '\n')); err != nil {
					return err
				}
				if flusher != nil && time.Now().UTC().After(flushEvery) {
					flusher.Flush()
					flushEvery = time.Now().UTC().Add(250 * time.Millisecond)
				}
				return nil
			}

			if mirror.store != nil {
				if _, err := mirror.store.Replay(r.Context(), sessionID, fromSeq, writeLine); err != nil {
					writeHTTPError(w, err)
					return
				}
			} else if sess, ok := mirror.getSession(sessionID); ok && sess != nil {
				if _, err := sess.replay(fromSeq, 0, writeLine); err != nil {
					writeHTTPError(w, err)
					return
				}
			} else {
				http.NotFound(w, r)
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
			return
		}
		if len(parts) == 2 && parts[1] == "tail" {
			if r.Method != http.MethodGet {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			ctx := r.Context()
			fromSeq := uint64(1)
			if raw := strings.TrimSpace(r.URL.Query().Get("from_sequence")); raw != "" {
				if n, err := strconv.ParseUint(raw, 10, 64); err == nil && n > 0 {
					fromSeq = n
				}
			}
			replay := true
			if raw := strings.TrimSpace(r.URL.Query().Get("replay")); raw != "" {
				switch strings.ToLower(raw) {
				case "0", "false", "no", "off":
					replay = false
				}
			}
			lastEventID := strings.TrimSpace(r.Header.Get("Last-Event-ID"))
			if lastEventID == "" {
				lastEventID = strings.TrimSpace(r.URL.Query().Get("last_event_id"))
			}
			if lastEventID != "" {
				if n, err := strconv.ParseUint(lastEventID, 10, 64); err == nil && n > 0 {
					fromSeq = n + 1
					replay = true
				}
			}

			exists := false
			if mirror.store != nil {
				if _, ok, err := mirror.store.GetSession(ctx, sessionID); err != nil {
					writeHTTPError(w, err)
					return
				} else if ok {
					exists = true
				}
			}
			if !exists {
				if _, ok := mirror.getSession(sessionID); ok {
					exists = true
				}
			}
			if !exists {
				http.NotFound(w, r)
				return
			}

			session := mirror.getOrCreateSession(ctx, sessionID)
			subscriber := session.subscribe()
			defer session.unsubscribe(subscriber)

			w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			w.Header().Set("X-Accel-Buffering", "no")

			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "streaming unsupported", http.StatusInternalServerError)
				return
			}

			mo := protojson.MarshalOptions{UseProtoNames: true}
			writeEvent := func(event string, id uint64, data []byte) error {
				if event == "" {
					event = "message"
				}
				if id > 0 {
					if _, err := fmt.Fprintf(w, "id: %d\n", id); err != nil {
						return err
					}
				}
				if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
					return err
				}
				flusher.Flush()
				return nil
			}

			retryMS := 1000
			if raw := strings.TrimSpace(firstQuery(r.URL.Query(), "retry_ms", "retry")); raw != "" {
				if n, err := strconv.Atoi(raw); err == nil && n > 0 {
					retryMS = n
				}
			}
			if retryMS < 250 {
				retryMS = 250
			}
			if retryMS > 60_000 {
				retryMS = 60_000
			}
			_, _ = fmt.Fprintf(w, "retry: %d\n\n", retryMS)
			flusher.Flush()

			heartbeat := 15 * time.Second
			if raw := strings.TrimSpace(firstQuery(r.URL.Query(), "heartbeat_ms")); raw != "" {
				if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
					heartbeat = time.Duration(n) * time.Millisecond
				}
			} else if raw := strings.TrimSpace(firstQuery(r.URL.Query(), "heartbeat")); raw != "" {
				if d, err := parseDurationOrMillis(raw); err == nil {
					heartbeat = d
				}
			}
			if heartbeat > 0 && heartbeat < time.Second {
				heartbeat = time.Second
			}
			if heartbeat > 5*time.Minute {
				heartbeat = 5 * time.Minute
			}

			nextSeq := uint64(0)
			if replay {
				nextSeq = fromSeq
			}
			emitDropped := func(from, to uint64, reason string) error {
				if from == 0 || to < from {
					return nil
				}
				type dropped struct {
					SessionID    string `json:"session_id"`
					FromSequence uint64 `json:"from_sequence"`
					ToSequence   uint64 `json:"to_sequence"`
					Missing      uint64 `json:"missing"`
					Reason       string `json:"reason,omitempty"`
					AtUnixNano   int64  `json:"at_unix_nano"`
				}
				payload := dropped{
					SessionID:    sessionID,
					FromSequence: from,
					ToSequence:   to,
					Missing:      to - from + 1,
					Reason:       strings.TrimSpace(reason),
					AtUnixNano:   time.Now().UTC().UnixNano(),
				}
				raw, err := json.Marshal(payload)
				if err != nil {
					return err
				}
				if err := writeEvent("dropped", to, raw); err != nil {
					return err
				}
				if nextSeq > 0 && to+1 > nextSeq {
					nextSeq = to + 1
				}
				return nil
			}
			sendFrame := func(frame *apiv1.MirrorFrame) error {
				if frame == nil {
					return nil
				}
				seq := frame.GetSequence()
				if seq == 0 {
					return nil
				}
				if nextSeq == 0 {
					nextSeq = seq
				}
				if nextSeq > 0 && seq < nextSeq {
					return nil
				}
				if nextSeq > 0 && seq > nextSeq {
					if err := emitDropped(nextSeq, seq-1, "not_available"); err != nil {
						return err
					}
				}
				raw, err := mo.Marshal(frame)
				if err != nil {
					return err
				}
				if err := writeEvent("frame", seq, raw); err != nil {
					return err
				}
				nextSeq = seq + 1
				return nil
			}
			replayRange := func(from, to uint64) error {
				if from == 0 || to < from {
					return nil
				}
				if mirror.store != nil {
					_, err := mirror.store.Replay(ctx, sessionID, from, func(frame *apiv1.MirrorFrame) error {
						if frame == nil {
							return nil
						}
						seq := frame.GetSequence()
						if seq == 0 {
							return nil
						}
						if seq > to {
							return errReplayStop
						}
						return sendFrame(frame)
					})
					if err != nil && !errors.Is(err, errReplayStop) {
						return err
					}
					return nil
				}
				_, err := session.replay(from, to, func(frame *apiv1.MirrorFrame) error {
					return sendFrame(frame)
				})
				return err
			}

			// Bound the replay snapshot to avoid replay/live duplicates.
			snapshotSeq := session.currentSeq()

			if replay {
				if err := replayRange(fromSeq, snapshotSeq); err != nil {
					if errors.Is(err, errReplayStop) {
						// ignore
					} else {
						writeHTTPError(w, err)
						return
					}
				}
			}

			// Keep-alive pings help proxies keep the stream open.
			var ping *time.Ticker
			if heartbeat > 0 {
				ping = time.NewTicker(heartbeat)
				defer ping.Stop()
			}
			var pingC <-chan time.Time
			if ping != nil {
				pingC = ping.C
			}

			for {
				select {
				case <-ctx.Done():
					return
				case <-pingC:
					_ = writeEvent("ping", 0, []byte(strconv.FormatInt(time.Now().UTC().UnixNano(), 10)))
				case frame, ok := <-subscriber.ch:
					if !ok {
						return
					}
					if frame == nil {
						continue
					}
					seq := frame.GetSequence()
					if seq < fromSeq {
						continue
					}
					if nextSeq > 0 && seq < nextSeq {
						continue
					}
					if nextSeq > 0 && seq > nextSeq {
						if err := replayRange(nextSeq, seq-1); err != nil && !errors.Is(err, errReplayStop) {
							writeHTTPError(w, err)
							return
						}
						if nextSeq > 0 && nextSeq < seq {
							if err := emitDropped(nextSeq, seq-1, "not_available"); err != nil {
								return
							}
						}
					}
					if err := sendFrame(frame); err != nil {
						return
					}
				}
			}
		}
		http.NotFound(w, r)
	})

	return mux
}

func firstQuery(q url.Values, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(firstNonEmpty(q[k])); v != "" {
			return v
		}
	}
	return ""
}

func parseDurationOrMillis(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("empty duration")
	}
	digits := true
	for _, r := range raw {
		if r < '0' || r > '9' {
			digits = false
			break
		}
	}
	if digits {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return 0, err
		}
		if n < 0 {
			return 0, fmt.Errorf("negative duration")
		}
		return time.Duration(n) * time.Millisecond, nil
	}
	return time.ParseDuration(raw)
}

func allowCORS(w http.ResponseWriter, r *http.Request) bool {
	if w == nil || r == nil {
		return false
	}
	// Default to permissive CORS for local dev UIs; real deployments should prefer
	// a reverse proxy that applies tighter CORS rules.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "authorization,x-ktl-token,last-event-id,content-type")
	w.Header().Set("Access-Control-Allow-Methods", "GET,POST,DELETE,OPTIONS")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return false
	}
	return true
}

func requireHTTPAuth(w http.ResponseWriter, r *http.Request, expected string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return true
	}
	if w == nil || r == nil {
		return false
	}
	raw := strings.TrimSpace(r.Header.Get("Authorization"))
	if raw == "" {
		raw = strings.TrimSpace(r.Header.Get("X-KTL-Token"))
	}
	if raw == "" {
		if c, err := r.Cookie(httpAuthCookieName); err == nil && c != nil {
			raw = strings.TrimSpace(c.Value)
		}
	}
	if raw == "" {
		http.Error(w, "missing authentication token", http.StatusUnauthorized)
		return false
	}
	token := raw
	if len(token) >= 7 && strings.EqualFold(token[:7], "bearer ") {
		token = strings.TrimSpace(token[7:])
	}
	if token == "" {
		http.Error(w, "missing authentication token", http.StatusUnauthorized)
		return false
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
		http.Error(w, "invalid authentication token", http.StatusUnauthorized)
		return false
	}
	return true
}

func writeProtoJSON(w http.ResponseWriter, msg proto.Message) {
	if w == nil {
		return
	}
	if msg == nil {
		http.Error(w, "missing response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	mo := protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: false}
	raw, err := mo.Marshal(msg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(raw)
}

func writeHTTPError(w http.ResponseWriter, err error) {
	if w == nil {
		return
	}
	if err == nil {
		return
	}
	if errors.Is(err, context.Canceled) {
		http.Error(w, "request canceled", http.StatusRequestTimeout)
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		http.Error(w, "request deadline exceeded", http.StatusGatewayTimeout)
		return
	}
	if st, ok := status.FromError(err); ok {
		code := http.StatusInternalServerError
		switch st.Code() {
		case codes.InvalidArgument:
			code = http.StatusBadRequest
		case codes.Unauthenticated:
			code = http.StatusUnauthorized
		case codes.PermissionDenied:
			code = http.StatusForbidden
		case codes.NotFound:
			code = http.StatusNotFound
		case codes.Unavailable:
			code = http.StatusServiceUnavailable
		}
		http.Error(w, st.Message(), code)
		return
	}
	http.Error(w, fmt.Sprintf("error: %v", err), http.StatusInternalServerError)
}
