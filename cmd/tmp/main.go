package main

import (
    "context"
    "fmt"
    "time"

    "github.com/go-logr/logr"
    "github.com/gorilla/websocket"

    "github.com/example/ktl/internal/caststream"
    "github.com/example/ktl/internal/deploy"
)

func main() {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    addr := "127.0.0.1:18086"
    stream := deploy.NewStreamBroadcaster("rel", "ns", "chart")
    srv := caststream.New(addr, caststream.ModeWeb, "Test", logr.Discard(), caststream.WithDeployUI())
    stream.AddObserver(srv)
    go func() {
        if err := srv.Run(ctx); err != nil {
            fmt.Println("server err", err)
        }
    }()
    time.Sleep(500 * time.Millisecond)

    u := "ws://" + addr + "/ws"
    conn, _, err := websocket.DefaultDialer.Dial(u, nil)
    if err != nil {
        panic(err)
    }
    defer conn.Close()

    stream.EmitSummary(deploy.SummaryPayload{Release: "rel", Namespace: "ns", Chart: "chart"})

    conn.SetReadDeadline(time.Now().Add(2 * time.Second))
    _, msg, err := conn.ReadMessage()
    if err != nil {
        panic(err)
    }
    fmt.Println(string(msg))

    fmt.Println("server running; press Ctrl+C to exit")
    select {
    case <-time.After(10 * time.Second):
    case <-ctx.Done():
    }
}
