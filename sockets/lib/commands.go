package sockets

import "strings"

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
	token    string
	paramstr string
	count    int
	target   CommandIO
}

type CommandIO interface {
	Process(Command) (err error)
}

func NewCommand(msg string, target CommandIO) Command {
	var count int
	token := string(msg[:1])
	paramstr := msg[1:]

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

func (c Command) Process() {
	c.target.Process(c)
}
