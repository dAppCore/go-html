package p2p

import (
	"context"
	"net"
	"time"

	core "dappco.re/go"
)

// A subscribing driver bound to a real loopback address accepts an inbound
// peer connection, frames the newline-delimited envelope and dispatches it to
// the subscriber — exercising ensureListener -> acceptLoop -> readConn.
//
//	receiver := NewTCPDriver(TCPOptions{ListenAddr: "127.0.0.1:0"})
//	receiver.Subscribe(ctx, "updates", handler)
//	sender.Publish(ctx, Envelope{Topic: "updates"}) // dials receiver's addr
func TestTCPDriverLoopback_AcceptRead_Good(t *core.T) {
	receiver := NewTCPDriver(TCPOptions{ListenAddr: "127.0.0.1:0", NodeID: "receiver"})
	defer receiver.Close()

	received := make(chan Envelope, 1)
	ctx := t.Context()

	core.RequireNoError(t, receiver.Subscribe(ctx, "updates", func(e Envelope) {
		received <- e
	}))

	// The listener is now live on an ephemeral port.
	addr := receiver.ListenAddr()
	core.AssertNotEqual(t, "127.0.0.1:0", addr)

	sender := NewTCPDriver(TCPOptions{PeerAddrs: []string{addr}, NodeID: "sender"})
	defer sender.Close()

	core.RequireNoError(t, sender.Publish(context.Background(), Envelope{
		Topic:    "updates",
		SenderID: "sender",
		Payload:  map[string]any{"hello": "world"},
	}))

	select {
	case got := <-received:
		core.AssertEqual(t, "updates", got.Topic)
		core.AssertEqual(t, "sender", got.SenderID)
		core.AssertEqual(t, "world", got.Payload["hello"])
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the loopback envelope")
	}
}

// readConn skips a malformed line without dropping the connection: a junk line
// is ignored while a subsequent well-formed envelope still dispatches.
func TestTCPDriverLoopback_AcceptRead_Bad(t *core.T) {
	receiver := NewTCPDriver(TCPOptions{ListenAddr: "127.0.0.1:0"})
	defer receiver.Close()

	received := make(chan Envelope, 1)
	ctx := t.Context()
	core.RequireNoError(t, receiver.Subscribe(ctx, "topic", func(e Envelope) {
		received <- e
	}))

	addr := receiver.ListenAddr()
	conn, err := net.Dial("tcp", addr)
	core.RequireNoError(t, err)
	defer conn.Close()

	// First a malformed line, then a valid envelope.
	_, err = conn.Write([]byte("not-json\n"))
	core.RequireNoError(t, err)
	valid, marshalErr := jsonMarshal(Envelope{Topic: "topic", SenderID: "peer"})
	core.RequireNoError(t, marshalErr)
	_, err = conn.Write(append(valid, '\n'))
	core.RequireNoError(t, err)

	select {
	case got := <-received:
		core.AssertEqual(t, "peer", got.SenderID)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the valid envelope after a malformed line")
	}
}
