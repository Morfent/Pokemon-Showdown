package sockets

import (
	"net"
	"os"
	"testing"

	"github.com/igm/sockjs-go/sockjs"
)

type testSocket struct {
	sockjs.Session
}

func (ts testSocket) Send(msg string) error {
	return nil
}

func (ts testSocket) Close(code uint32, signal string) error {
	return nil
}

func TestMasterListen(t *testing.T) {
	t.Parallel()
	ln, _ := net.Listen("tcp", ":3000")
	defer ln.Close()

	envVar := "PS_IPC_PORT"
	os.Setenv(envVar, ":3000")
	conn, _ := NewConnection(envVar)
	defer conn.Close()
	mux := NewMultiplexer()
	mux.Listen(conn)
	conn.Listen(mux)

	m := NewMaster(4)
	m.Spawn()
	go m.Listen()

	for i := 0; i < m.count*250; i++ {
		id := string(i)
		t.Run("Worker/Multiplexer command #"+id, func(t *testing.T) {
			go func(id string, mux *Multiplexer, conn *Connection) {
				mux.smux.Lock()
				sid := string(mux.nsid)
				mux.sockets[sid] = testSocket{}
				mux.nsid++
				mux.smux.Unlock()

				cmd := BuildCommand(SOCKET_DISCONNECT, sid, mux)
				cmd.Process()
				if len(CmdQueue) != 0 {
					t.Error("Sockets: master failed to pass command struct from worker to multiplexer")
				}
			}(id, mux, conn)
		})
		t.Run("Worker/Connection command #"+id, func(t *testing.T) {
			go func(id string, mux *Multiplexer, conn *Connection) {
				mux.smux.Lock()
				sid := string(mux.nsid)
				mux.smux.Unlock()

				cmd := BuildCommand(SOCKET_CONNECT, sid+"\n0.0.0.0\n\nwebsocket", conn)
				cmd.Process()
				if len(CmdQueue) != 0 {
					t.Error("Sockets: master failed to pass command struct from worker to connection")
				}
			}(id, mux, conn)
		})
	}

	for len(m.wpool) > 0 {
		<-m.wpool
	}
}
