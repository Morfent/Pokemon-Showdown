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
const (
	SOCKET_CONNECT       byte = '*'
	SOCKET_DISCONNECT    byte = '!'
	SOCKET_RECEIVE       byte = '<'
	SOCKET_SEND          byte = '>'
	CHANNEL_ADD          byte = '+'
	CHANNEL_REMOVE       byte = '-'
	CHANNEL_BROADCAST    byte = '#'
	SUBCHANNEL_MOVE      byte = '.'
	SUBCHANNEL_BROADCAST byte = ':'
)

type Command struct {
	token    byte      // Token designating the type of command.
	paramstr string    // The command parametre list, unparsed.
	count    int       // The number of parametres in the paramstr.
	target   CommandIO // The target to process this command.
}

// The multiplexer and the IPC connection both implement this interface. Its
// purpose is solely to allow the two structs to be used in Command.
type CommandIO interface {
	Process(Command) error // Invokes one of its methods using the command's token and parametres.
}

func getCount(token byte) (count int) {
	// Get the number of params in the paramstr based on the token type.
	// Command. Params uses this to quickly get a slice of params, so the
	// multiplexer and the IPC connection don't have to parse the paramstr
	// themselves.
	switch token {
	case SOCKET_DISCONNECT:
		count = 1
	case SOCKET_RECEIVE, SOCKET_SEND, CHANNEL_ADD, CHANNEL_REMOVE,
		CHANNEL_BROADCAST, SUBCHANNEL_BROADCAST:
		count = 2
	case SUBCHANNEL_MOVE:
		count = 3
	case SOCKET_CONNECT:
		count = 4
	}

	return
}

func NewCommand(msg string, target CommandIO) Command {
	token := msg[0]
	paramstr := msg[1:]
	count := getCount(token)
	return Command{
		token:    token,
		paramstr: paramstr,
		count:    count,
		target:   target,
	}
}

func BuildCommand(token byte, paramstr string, target CommandIO) Command {
	count := getCount(token)
	return Command{
		token:    token,
		paramstr: paramstr,
		count:    count,
		target:   target,
	}
}

func (c Command) Token() byte {
	return c.token
}

func (c Command) Params() []string {
	return strings.SplitN(c.paramstr, "\n", c.count)
}

func (c Command) Message() string {
	return string(c.token) + c.paramstr
}

func (c Command) Process() error {
	return c.target.Process(c)
}
