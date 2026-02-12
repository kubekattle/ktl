package agent

import (
	"context"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kubekattle/ktl/internal/grpcutil"
	"github.com/kubekattle/ktl/internal/workflows/buildsvc"
	apiv1 "github.com/kubekattle/ktl/pkg/api/ktl/api/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestMirrorFlightRecorder_PersistReplayListExport(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "mirror.sqlite")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srvStopped := false
	srv, err := New(Config{MirrorStore: storePath}, buildsvc.New(buildsvc.Dependencies{}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- srv.Serve(ctx, ln)
	}()
	t.Cleanup(func() {
		if srvStopped {
			return
		}
		cancel()
		select {
		case <-serveDone:
		case <-time.After(2 * time.Second):
			t.Fatalf("agent server did not stop in time")
		}
	})

	addr := ln.Addr().String()
	dialCtx, cancelDial := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelDial()
	conn, err := grpcutil.Dial(dialCtx, addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	client := apiv1.NewMirrorServiceClient(conn)

	// Publish a few frames and verify sequences.
	{
		pubCtx, cancelPub := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelPub()
		stream, err := client.Publish(pubCtx)
		if err != nil {
			t.Fatalf("Publish: %v", err)
		}
		sessionID := "sess1"
		for i := 0; i < 3; i++ {
			if err := stream.Send(&apiv1.MirrorFrame{
				SessionId: sessionID,
				Producer:  "test",
				Payload: &apiv1.MirrorFrame_Log{Log: &apiv1.LogLine{
					TimestampUnixNano: time.Now().UTC().UnixNano(),
					Namespace:         "ns",
					Pod:               "pod",
					Container:         "c",
					Raw:               "line",
				}},
			}); err != nil {
				t.Fatalf("Send[%d]: %v", i, err)
			}
			ack, err := stream.Recv()
			if err != nil {
				t.Fatalf("RecvAck[%d]: %v", i, err)
			}
			if got := ack.GetSessionId(); got != sessionID {
				t.Fatalf("ack session_id: got %q want %q", got, sessionID)
			}
			if got, want := ack.GetSequence(), uint64(i+1); got != want {
				t.Fatalf("ack sequence[%d]: got %d want %d", i, got, want)
			}
		}
		_ = stream.CloseSend()
	}

	// Attach metadata/tags (used by UIs/IDEs for indexing).
	{
		metaCtx, cancelMeta := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelMeta()
		_, err := client.SetSessionMeta(metaCtx, &apiv1.MirrorSetSessionMetaRequest{
			SessionId: "sess1",
			Meta: &apiv1.MirrorSessionMeta{
				Command:   "ktl logs",
				Args:      []string{"checkout-.*", "--namespace", "ns"},
				Requester: "tester",
				Namespace: "ns",
				Release:   "rel",
				Chart:     "./chart",
			},
			Tags: map[string]string{
				"team": "infra",
			},
		})
		if err != nil {
			t.Fatalf("SetSessionMeta: %v", err)
		}
	}

	// Restart the server to prove durability.
	cancel()
	select {
	case <-serveDone:
		srvStopped = true
	case <-time.After(3 * time.Second):
		t.Fatalf("agent server did not stop in time")
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	srv2, err := New(Config{MirrorStore: storePath}, buildsvc.New(buildsvc.Dependencies{}))
	if err != nil {
		t.Fatalf("New (restart): %v", err)
	}
	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen (restart): %v", err)
	}
	t.Cleanup(func() { _ = ln2.Close() })

	serveDone2 := make(chan error, 1)
	go func() {
		serveDone2 <- srv2.Serve(ctx2, ln2)
	}()
	t.Cleanup(func() {
		cancel2()
		select {
		case <-serveDone2:
		case <-time.After(2 * time.Second):
			t.Fatalf("agent server (restart) did not stop in time")
		}
	})

	addr2 := ln2.Addr().String()
	dialCtx2, cancelDial2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelDial2()
	conn2, err := grpcutil.Dial(dialCtx2, addr2, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial (restart): %v", err)
	}
	defer conn2.Close()

	client2 := apiv1.NewMirrorServiceClient(conn2)

	// List sessions should include sess1 from the store.
	{
		listCtx, cancelList := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelList()
		resp, err := client2.ListSessions(listCtx, &apiv1.MirrorListSessionsRequest{Limit: 20})
		if err != nil {
			t.Fatalf("ListSessions: %v", err)
		}
		found := false
		for _, s := range resp.GetSessions() {
			if s.GetSessionId() == "sess1" {
				found = true
				if s.GetLastSequence() < 3 {
					t.Fatalf("sess1 last_sequence: got %d want >=3", s.GetLastSequence())
				}
				if s.GetMeta().GetCommand() != "ktl logs" {
					t.Fatalf("sess1 meta.command: got %q want %q", s.GetMeta().GetCommand(), "ktl logs")
				}
				if s.GetTags()["team"] != "infra" {
					t.Fatalf("sess1 tag team: got %q want %q", s.GetTags()["team"], "infra")
				}
			}
		}
		if !found {
			t.Fatalf("expected sess1 in ListSessions, got %+v", resp.GetSessions())
		}
	}

	// GetSession should return the session metadata.
	{
		getCtx, cancelGet := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelGet()
		sess, err := client2.GetSession(getCtx, &apiv1.MirrorGetSessionRequest{SessionId: "sess1"})
		if err != nil {
			t.Fatalf("GetSession: %v", err)
		}
		if sess.GetMeta().GetCommand() != "ktl logs" {
			t.Fatalf("GetSession meta.command: got %q want %q", sess.GetMeta().GetCommand(), "ktl logs")
		}
		if sess.GetTags()["team"] != "infra" {
			t.Fatalf("GetSession tag team: got %q want %q", sess.GetTags()["team"], "infra")
		}
	}

	// Subscribe replay should return the persisted frames with sequences.
	{
		subCtx, cancelSub := context.WithCancel(context.Background())
		defer cancelSub()
		stream, err := client2.Subscribe(subCtx, &apiv1.MirrorSubscribeRequest{SessionId: "sess1", Replay: true, FromSequence: 1})
		if err != nil {
			t.Fatalf("Subscribe: %v", err)
		}
		for i := 0; i < 3; i++ {
			frame, err := stream.Recv()
			if err != nil {
				t.Fatalf("Recv[%d]: %v", i, err)
			}
			if got, want := frame.GetSequence(), uint64(i+1); got != want {
				t.Fatalf("frame sequence[%d]: got %d want %d", i, got, want)
			}
			if frame.GetReceivedUnixNano() == 0 {
				t.Fatalf("frame received_unix_nano[%d] was 0", i)
			}
		}
		cancelSub()
	}

	// Export should return JSONL.
	{
		expCtx, cancelExp := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelExp()
		stream, err := client2.Export(expCtx, &apiv1.MirrorExportRequest{SessionId: "sess1", Format: "jsonl"})
		if err != nil {
			t.Fatalf("Export: %v", err)
		}
		var b strings.Builder
		for {
			chunk, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("Export Recv: %v", err)
			}
			b.Write(chunk.GetData())
		}
		txt := b.String()
		if !strings.Contains(txt, "\"session_id\":\"sess1\"") {
			t.Fatalf("expected export to include sess1, got: %s", txt)
		}
	}

	// Publishing to the same session after restart should continue the sequence.
	{
		pubCtx, cancelPub := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelPub()
		stream, err := client2.Publish(pubCtx)
		if err != nil {
			t.Fatalf("Publish (restart): %v", err)
		}
		if err := stream.Send(&apiv1.MirrorFrame{SessionId: "sess1", Producer: "test", Payload: &apiv1.MirrorFrame_Raw{Raw: []byte("x")}}); err != nil {
			t.Fatalf("Send (restart): %v", err)
		}
		ack, err := stream.Recv()
		if err != nil {
			t.Fatalf("RecvAck (restart): %v", err)
		}
		if got, want := ack.GetSequence(), uint64(4); got != want {
			t.Fatalf("sequence continuity: got %d want %d", got, want)
		}
		_ = stream.CloseSend()
	}

	// DeleteSession should remove it from the store.
	{
		delCtx, cancelDel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelDel()
		resp, err := client2.DeleteSession(delCtx, &apiv1.MirrorDeleteSessionRequest{SessionId: "sess1"})
		if err != nil {
			t.Fatalf("DeleteSession: %v", err)
		}
		if !resp.GetDeleted() {
			t.Fatalf("DeleteSession deleted: got false want true")
		}
		_, err = client2.GetSession(delCtx, &apiv1.MirrorGetSessionRequest{SessionId: "sess1"})
		if err == nil {
			t.Fatalf("expected GetSession to fail after delete")
		}
	}
}
