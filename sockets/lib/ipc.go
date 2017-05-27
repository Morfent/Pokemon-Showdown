package sockets

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
)

const DELIM byte = '\u0003'

type Connection struct {
	port      string
	addr      *net.TCPAddr
	conn      *net.TCPConn
	mux       *Multiplexer
	listening bool
}

func NewConnection(envVar string) (c *Connection, err error) {
	port := os.Getenv(envVar)
	addr, err := net.ResolveTCPAddr("tcp", "localhost"+port)
	if err != nil {
		return nil, fmt.Errorf("Sockets: failed to parse TCP address to connect to the parent process with: %v", err)
	}

	// IPv6 is perfectly fine to use here if the machine running this supports
	// it. The HTTP(S) server can't use it because PS is dependent on user IPs
	// exclusively being IPv4.
	conn, err := net.DialTCP("tcp", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("Sockets: failed to connect to TCP server: %v", err)
	}

	c = &Connection{
		port:      port,
		addr:      addr,
		conn:      conn,
		listening: false}

	return
}

func (c *Connection) Listening() bool {
	return c.listening
}

func (c *Connection) Listen(mux *Multiplexer) {
	if c.listening {
		return
	}

	c.mux = mux
	c.listening = true

	go func() {
		reader := bufio.NewReader(c.conn)
		for {
			var token []byte
			token, err := reader.ReadBytes(DELIM)
			if len(token) == 0 || err != nil {
				continue
			}

			var msg string
			err = json.Unmarshal(token[:len(token)-1], &msg)
			cmd := NewCommand(msg, c.mux)
			CmdQueue <- cmd
		}
	}()

	return
}

func (c *Connection) Process(cmd Command) (err error) {
	// fmt.Printf("Sockets => IPC: %v\n", cmd.Message())
	if !c.listening {
		return fmt.Errorf("Sockets: can't process connection commands when the connection isn't listening yet")
	}

	msg := cmd.Message()
	_, err = c.Write(msg)
	return
}

func (c *Connection) Close() error {
	return c.conn.Close()
}

func (c *Connection) Write(message string) (int, error) {
	if !c.listening {
		return 0, fmt.Errorf("Sockets: can't write messages over a connection that isn't listening yet...")
	}

	msg, err := json.Marshal(message)
	if err != nil {
		return 0, fmt.Errorf("Sockets: failed to parse upstream IPC message: %v", err)
	}
	return c.conn.Write(append(msg, DELIM))
}
