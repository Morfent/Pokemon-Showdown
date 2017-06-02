package sockets

import (
	"fmt"
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
			t.Errorf("Sockets: expected sockets length to be %v, but is actually %v", 1, len(mux.sockets))
		}
		mux.socketRemove(sid, true)
	})
	t.Run("*Multiplexer.socketRemove", func(t *testing.T) {
		sid := mux.socketAdd(testSocket{})
		if err := mux.socketRemove(sid, true); err != nil {
			t.Errorf("%v", err)
		}
		if len(mux.sockets) != 0 {
			t.Errorf("Sockets: expected sockets length to be %v, but is actually %v", 0, len(mux.sockets))
		}
		sid = mux.socketAdd(testSocket{})
		if err := mux.socketRemove(sid, false); err != nil {
			t.Errorf("%v", err)
		}
		if len(mux.sockets) != 0 {
			t.Errorf("Sockets: expected sockets length to be %v, but is actually %v", 0, len(mux.sockets))
		}
	})
	t.Run("*Multiplexer.channelAdd", func(t *testing.T) {
		sid := mux.socketAdd(testSocket{})
		cid := "global"
		if err := mux.channelAdd(cid, sid); err != nil {
			t.Errorf("%v", err)
		}
		if len(mux.channels) != 1 {
			t.Errorf("Sockets: expected channels length to be %v, but is actually %v", 1, len(mux.channels))
		}
		if err := mux.channelAdd(cid, sid); err != nil {
			t.Errorf("%v", err)
		}
		mux.channelRemove(cid, sid)
		mux.socketRemove(sid, true)
	})
	t.Run("*Multiplexer.channelRemove", func(t *testing.T) {
		sid := mux.socketAdd(testSocket{})
		cid := "global"
		mux.channelAdd(cid, sid)
		if err := mux.channelRemove(cid, sid); err != nil {
			t.Errorf("%v", err)
		}
		if len(mux.channels) != 0 {
			t.Errorf("Sockets: expected channels length to be %v, but is actually %v", 0, len(mux.channels))
		}
		if err := mux.channelRemove(cid, sid); err != nil {
			t.Errorf("%v", err)
		}
		mux.socketRemove(sid, true)
	})
	t.Run("*Multiplexer.channelBroadcast", func(t *testing.T) {
		sid := mux.socketAdd(testSocket{})
		cid := "global"
		mux.channelAdd(cid, sid)
		if err := mux.channelBroadcast(cid, "|raw|ayy lmao"); err != nil {
			t.Errorf("%v", err)
		}
		mux.channelRemove(cid, sid)
		if err := mux.channelBroadcast(cid, "|raw|ayy lmao"); err != nil {
			t.Errorf("%v", err)
		}
		mux.socketRemove(sid, true)
	})
	t.Run("*Multiplexer.subchannelMove", func(t *testing.T) {
		sid := mux.socketAdd(testSocket{})
		cid := "global"
		mux.channelAdd(cid, sid)
		if err := mux.subchannelMove(cid, P1_SUBCHANNEL_ID, sid); err != nil {
			t.Errorf("%v", err)
		}
		if scid := mux.channels[cid][sid]; scid != P1_SUBCHANNEL_ID {
			t.Errorf("Sockets: expected subchannel for socket of ID %v in channel %v to be %v, but is actually %v", sid, cid, P1_SUBCHANNEL_ID, scid)
		}
		mux.channelRemove(cid, sid)
		mux.socketRemove(sid, true)
	})
	t.Run("*Multiplexer.subchannelBroadcast", func(t *testing.T) {
		msg := "|split\n0\n1\n2\n|\n|split\n3\n4\n5\n|"
		scids := []byte{DEFAULT_SUBCHANNEL_ID, P1_SUBCHANNEL_ID, P2_SUBCHANNEL_ID}
		for idx, scid := range scids {
			amsg := mux.scre.ReplaceAllString(msg, fmt.Sprintf("$%v", idx+1))
			if emsg := fmt.Sprintf("%v\n%v", idx, idx+3); emsg != amsg {
				t.Errorf("Sockets: expected broadcast to subchannel of ID %v to be %v, but is actually %v", string(scid), emsg, amsg)
			}
		}

		sid := mux.socketAdd(testSocket{})
		cid := "global"
		mux.channelAdd(cid, sid)
		mux.subchannelMove(cid, P1_SUBCHANNEL_ID, sid)
		if err := mux.subchannelBroadcast(cid, msg); err != nil {
			t.Errorf("%v", err)
		}
		mux.channelRemove(cid, sid)
		mux.socketRemove(sid, true)
	})
}
