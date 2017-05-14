package sockets

import (
	"net"
	"testing"
)

func TestMultiplexer(t *testing.T) {
	port := ":3000"
	ln, _ := net.Listen("tcp", "localhost"+port)
	defer ln.Close()
	conn, _ := NewConnection("PS_IPC_PORT")
	defer conn.Close()
	mux := NewMultiplexer()
	mux.Listen(conn)

	t.Run("*Multiplexer.socketAdd", func(t *testing.T) {
		sid := mux.socketAdd(testSocket{})
		if len(mux.sockets) != 1 {
			t.Errorf("Sockets: adding sockets to multiplexer doesn't keep them instead")
		}
		mux.socketRemove(sid, true)
	})
	t.Run("*Multiplexer.socketRemove", func(t *testing.T) {
		sid := mux.socketAdd(testSocket{})
		if err := mux.socketRemove(sid, true); err != nil {
			t.Errorf("%v", err)
		}
		if len(mux.sockets) != 0 {
			t.Errorf("Sockets: forcibly removing sockets from multiplexer keeps them instead")
		}
		sid = mux.socketAdd(testSocket{})
		if err := mux.socketRemove(sid, false); err != nil {
			t.Errorf("%v", err)
		}
		if len(mux.sockets) != 0 {
			t.Fatalf("Sockets: sockets removing themselves from multiplexer keeps them instead")
		}
	})
	t.Run("*Multiplexer.channelAdd", func(t *testing.T) {
		sid := mux.socketAdd(testSocket{})
		if err := mux.channelAdd("global", sid); err != nil {
			t.Errorf("%v", err)
		}
		if len(mux.channels) != 1 {
			t.Errorf("Sockets: adding channels to multiplexer doesn't keep them instead")
		}
		if err := mux.channelAdd("global", sid); err != nil {
			t.Errorf("%v", err)
		}
		mux.channelRemove("global", sid)
		mux.socketRemove(sid, true)
	})
	t.Run("*Multiplexer.channelRemove", func(t *testing.T) {
		sid := mux.socketAdd(testSocket{})
		mux.channelAdd("global", sid)
		if err := mux.channelRemove("global", sid); err != nil {
			t.Errorf("%v", err)
		}
		if len(mux.channels) != 0 {
			t.Errorf("Sockets: removing channels from multiplexer keeps them instead")
		}
		if err := mux.channelRemove("global", sid); err != nil {
			t.Errorf("%v", err)
		}
		mux.socketRemove(sid, true)
	})
	t.Run("*Multiplexer.channelBroadcast", func(t *testing.T) {
		sid := mux.socketAdd(testSocket{})
		mux.channelAdd("global", sid)
		if err := mux.channelBroadcast("global", "|raw|ayy lmao"); err != nil {
			t.Errorf("%v", err)
		}
		mux.channelRemove("global", sid)
		if err := mux.channelBroadcast("global", "|raw|ayy lmao"); err != nil {
			t.Errorf("%v", err)
		}
		mux.socketRemove(sid, true)
	})
	t.Run("*Multiplexer.subchannelMove", func(t *testing.T) {
		sid := mux.socketAdd(testSocket{})
		mux.channelAdd("global", sid)
		if err := mux.subchannelMove("global", P1_SUBCHANNEL_ID, sid); err != nil {
			t.Errorf("%v", err)
		}
		mux.channelRemove("global", sid)
		mux.socketRemove(sid, true)
	})
	t.Run("*Multiplexer.subchannelBroadcast", func(t *testing.T) {
		msg := "\n/split\n1\n2\n3\nrest"
		matches := mux.scre.FindAllStringSubmatch(msg, len(msg))
		msgs := matches[0]
		if msgs[1] != "\n1" {
			t.Errorf("Sockets: expected broadcast to subchannel '0' to be %v, but was actually %v", "\n1", msgs[1])
		}
		if msgs[2] != "\n2" {
			t.Errorf("Sockets: expected broadcast to subchannel '1' to be %v, but was actually %v", "\n2", msgs[2])
		}
		if msgs[3] != "\n3" {
			t.Errorf("Sockets: expected broadcast to subchannel '2' to be %v, but was actually %v", "\n3", msgs[3])
		}

		sid := mux.socketAdd(testSocket{})
		mux.channelAdd("global", sid)
		if err := mux.subchannelBroadcast("global", msg); err != nil {
			t.Errorf("%v", err)
		}
		mux.channelRemove("global", sid)
		mux.socketRemove(sid, true)
	})
}
