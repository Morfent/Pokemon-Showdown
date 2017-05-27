package sockets

import (
	"net"
	"os"
	"testing"
)

type testMux struct {
	CommandIO
}

func (tm *testMux) Listen(conn CommandIO) (err error) {
	return nil
}

func (tm *testMux) Process(cmd Command) (err error) {
	return nil
}

func TestConnection(t *testing.T) {
	port := ":3000"
	ln, err := net.Listen("tcp", "localhost"+port)
	defer ln.Close()
	if err != nil {
		t.Errorf("Sockets: failed to launch TCP server on port %v: %v", port, err)
	}

	envVar := "PS_IPC_PORT"
	err = os.Setenv(envVar, port)
	if err != nil {
		t.Errorf("Sockets: failed to set %v environment variable: %v", envVar, port)
	}

	conn, err := NewConnection(envVar)
	defer conn.Close()
	if err != nil {
		t.Errorf("%v", err)
	}
	if conn.port != port {
		t.Errorf("Sockets: new connection expected to have port %v but had %v instead", port, conn.port)
	}

	mux := NewMultiplexer()
	conn.Listen(mux)
	mux.Listen(conn)

	cmd := BuildCommand(SOCKET_SEND, "0\n|ayy lmao", mux)
	err = conn.Process(cmd)
	if err != nil {
		t.Errorf("%v", err)
	}

	bc, err := conn.Write(string(DELIM))
	if err != nil {
		t.Errorf("%v", err)
	}
	bc += 3 // For the escaped backslashes and additional DELIM character

	cbc := len([]byte(cmd.Message()))
	if bc != cbc {
		t.Errorf("Sockets: expected the number of bytes received by the connection to be %v, but actually received %v", bc, cbc)
	}
}
