/**
 * IPC - Inter-Process Communication
 * https://pokemonshowdown.com/
 *
 * This handles all communication between us and the parent process. The parent
 * process creates a local TCP server using a random port. The port is passed
 * down to us through the $PS_IPC_PORT environment variable. A TCP connection
 * to the parent's server is created, allowing us to send messages back and
 * forth.
 */

package sockets

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
)

// This must be a byte that stringifies to either a hexadecimal escape code.
// Otherwise, it would be possible for someone to send a message with the
// delimiter and break up messages.
const DELIM byte = '\x03'

type Connection struct {
	addr      *net.TCPAddr // Parent process' TCP server address.
	conn      *net.TCPConn // Connection to the parent process' TCP server.
	mux       *Multiplexer // Target for commands originating from here.
	listening bool         // Whether or not this is connected and listening for IPC messages.
}

func NewConnection(envVar string) (*Connection, error) {
	port := os.Getenv(envVar)
	addr, err := net.ResolveTCPAddr("tcp", "localhost"+port)
	if err != nil {
		return nil, fmt.Errorf("Sockets: failed to parse TCP address to connect to the parent process with: %v", err)
	}

	conn, err := net.DialTCP("tcp", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("Sockets: failed to connect to TCP server: %v", err)
	}

	c := &Connection{
		addr:      addr,
		conn:      conn,
		listening: false,
	}

	return c, nil
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
		scanner := bufio.NewScanner(c.conn)
		split := func(data []byte, atEOF bool) (advance int, token []byte, err error) {
			for i := 0; i < len(data); i++ {
				if data[i] == DELIM {
					return i + 1, data[:i], nil
				}
			}
			return 0, data, bufio.ErrFinalToken
		}
		scanner.Split(split)

		for scanner.Scan() {
			var msg string
			token := scanner.Bytes()
			err := json.Unmarshal(token, &msg)
			if err != nil {
				continue
			}

			cmd := NewCommand(msg, c.mux)
			CmdQueue <- cmd
		}
	}()
}

// Final step in evaluating commands targeted at the IPC connection.
func (c *Connection) Process(cmd Command) error {
	// fmt.Printf("Sockets => IPC: %v\n", cmd.Message())
	if !c.listening {
		return fmt.Errorf("Sockets: can't process connection commands when the connection isn't listening yet")
	}

	msg := cmd.Message()
	_, err := c.write(msg)
	return err
}

func (c *Connection) Close() error {
	if !c.listening {
		return nil
	}
	return c.conn.Close()
}

func (c *Connection) write(msg string) (int, error) {
	if !c.listening {
		return 0, fmt.Errorf("Sockets: can't write messages over a connection that isn't listening yet...")
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return 0, fmt.Errorf("Sockets: failed to parse upstream IPC message: %v", err)
	}

	var start, count int
	data = append(data, DELIM)
	for {
		count, err = c.conn.Write(data)
		start += count
		if err != nil {
			err = fmt.Errorf("Sockets: failed to write message to IPC connection: %v", err)
			break
		}

		if count == 0 || start == len(data) {
			break
		}
	}

	return start, err
}
