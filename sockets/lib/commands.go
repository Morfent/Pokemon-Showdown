/**
 * Commands
 * https://pokemonshowdown.com/
 *
 * Commands are an abstraction over IPC messages sent to and received from the
 * parent process. Each message follows a specific syntax: a one character
 * token, followed by any number of parametres separated by newlines. Commands
 * give the multiplexer and IPC connection a simple way to determine which
 * struct it's meant to be handled by, before enqueueing it to be distributed
 * to workers to finally process their payload concurrently.
 */

package sockets

import "strings"

// IPC message types
const SOCKET_CONNECT string = "*"
const SOCKET_DISCONNECT string = "!"
const SOCKET_RECEIVE string = "<"
const SOCKET_SEND string = ">"
const CHANNEL_ADD string = "+"
const CHANNEL_REMOVE string = "-"
const CHANNEL_BROADCAST string = "#"
const SUBCHANNEL_MOVE string = "."
const SUBCHANNEL_BROADCAST string = ":"

type Command struct {
	// The first character of the message. This signifies the command type.
	token string
	// The message with the token removed.
	paramstr string
	// The number of parametres in the paramstr. Necessary, since messages
	// may contain newlines of their own.
	count int
	// Either the multiplexer or the IPC connection. Used by workers to finally
	// process the payload using the target's Process method.
	target CommandIO
}

// The multiplexer and the IPC connection both implement this interface. Its
// purpose is solely to allow the two structs to be used in Command.
type CommandIO interface {
	// Parse the message. Update state with the command's params if need be,
	// then enqueue another command if a response needs to be sent to the IPC
	// connection.
	Process(Command) (err error)
}

func NewCommand(msg string, target CommandIO) Command {
	var count int
	token := string(msg[:1])
	paramstr := msg[1:]

	// Get the number of params in the paramstr based on the token type.
	// Cmmmand.Params uses this to quickly get a slice of params, so the
	// multiplexer and the IPC connection don't have to parse the paramstr
	// themselves.
	switch token {
	case SOCKET_DISCONNECT:
		count = 1
	case SOCKET_RECEIVE:
		count = 2
	case SOCKET_SEND:
		count = 2
	case CHANNEL_ADD:
		count = 2
	case CHANNEL_REMOVE:
		count = 2
	case CHANNEL_BROADCAST:
		count = 2
	case SUBCHANNEL_BROADCAST:
		count = 2
	case SUBCHANNEL_MOVE:
		count = 3
	case SOCKET_CONNECT:
		count = 4
	}

	return Command{
		token:    token,
		paramstr: paramstr,
		count:    count,
		target:   target}
}

func (c Command) Token() string {
	return c.token
}

func (c Command) Params() []string {
	return strings.SplitN(c.paramstr, "\n", c.count)
}

func (c Command) Message() string {
	return c.token + c.paramstr
}

// The command target here is ALWAYS the multiplexer. This is the final step in
// getting a message parsed, which is after a worker has received this command.
func (c Command) Process() {
	c.target.Process(c)
}
