/**
 * Multiplexer - Socket/Channel/Subchannel state machine
 * https://pokemonshowdown.com/
 *
 * This keeps track of the sockets that connect to the SockJS server. Sockets
 * are stored in the multiplexer to allow the parent process to manipulate them
 * as it pleases. Channels represent rooms in the parent process; subchannels
 * split battle rooms into three groups: side 1, side 2, and spectators.
 * Certain messages will display differently depending on which subchannel the
 * user's socket is in.
 */

package sockets

import (
	"fmt"
	"net"
	"path"
	"regexp"
	"strconv"
	"sync"

	"github.com/igm/sockjs-go/sockjs"
)

// Subchannel IDs
const (
	DEFAULT_SUBCHANNEL_ID byte = '0'
	P1_SUBCHANNEL_ID      byte = '1'
	P2_SUBCHANNEL_ID      byte = '2'
)

// Map of socket IDs to subchannel IDs.
type Channel map[string]byte

type Multiplexer struct {
	nsid     uint64                    // Socket ID counter.
	sockets  map[string]sockjs.Session // Map of socket IDs to sockets.
	smux     sync.Mutex                // nsid and sockets mutex.
	channels map[string]Channel        // Map of channel (i.e. room) IDs to channels.
	cmux     sync.Mutex                // channels mutex.
	scre     *regexp.Regexp            // Regex for splitting subchannel broadcasts into their three messages.
	conn     *Connection               // Target for commands originating from here.
}

func NewMultiplexer() *Multiplexer {
	sockets := make(map[string]sockjs.Session)
	channels := make(map[string]Channel)
	scre := regexp.MustCompile(`\|split\n([^\n]*)\n([^\n]*)\n([^\n]*)\n[^\n]*`)
	return &Multiplexer{
		sockets:  sockets,
		channels: channels,
		scre:     scre,
	}
}

func (m *Multiplexer) Listen(conn *Connection) {
	m.conn = conn
}

func (m *Multiplexer) Process(cmd Command) (err error) {
	// fmt.Printf("IPC => Sockets: %v\n", cmd.Message())
	params := cmd.Params()

	// Parse the command's params and call the appropriate method.
	switch token := cmd.Token(); token {
	case SOCKET_DISCONNECT:
		sid := params[0]
		err = m.socketRemove(sid, true)
	case SOCKET_SEND:
		sid := params[0]
		msg := params[1]
		err = m.socketSend(sid, msg)
	case SOCKET_RECEIVE:
		sid := params[0]
		msg := params[1]
		err = m.socketReceive(sid, msg)
	case CHANNEL_ADD:
		cid := params[0]
		sid := params[1]
		err = m.channelAdd(cid, sid)
	case CHANNEL_REMOVE:
		cid := params[0]
		sid := params[1]
		err = m.channelRemove(cid, sid)
	case CHANNEL_BROADCAST:
		cid := params[0]
		msg := params[1]
		err = m.channelBroadcast(cid, msg)
	case SUBCHANNEL_MOVE:
		cid := params[0]
		scid := params[1][0]
		sid := params[2]
		err = m.subchannelMove(cid, scid, sid)
	case SUBCHANNEL_BROADCAST:
		cid := params[0]
		msg := params[1]
		err = m.subchannelBroadcast(cid, msg)
	}

	if err != nil {
		// Something went wrong somewhere, but it's likely a timing issue from
		// the parent process. Let's just log the error instead of crashing.
		fmt.Printf("%v\n", err)
	}

	return
}

func (m *Multiplexer) socketAdd(s sockjs.Session) (sid string) {
	m.smux.Lock()
	defer m.smux.Unlock()

	sid = strconv.FormatUint(m.nsid, 10)
	m.nsid++
	m.sockets[sid] = s

	if m.conn.Listening() {
		req := s.Request()
		ip, _, _ := net.SplitHostPort(req.RemoteAddr)
		ips := req.Header.Get("X-Forwarded-For")
		protocol := path.Base(req.URL.Path)

		cmd := BuildCommand(SOCKET_CONNECT, sid+"\n"+ip+"\n"+ips+"\n"+protocol, m.conn)
		CmdQueue <- cmd
	}

	return
}

func (m *Multiplexer) socketRemove(sid string, forced bool) error {
	m.smux.Lock()
	defer m.smux.Unlock()

	m.cmux.Lock()
	for cid, c := range m.channels {
		if _, ok := c[sid]; ok {
			delete(c, sid)
			if len(c) == 0 {
				delete((*m).channels, cid)
			}
		}
	}
	m.cmux.Unlock()

	s, ok := m.sockets[sid]
	if ok {
		delete((*m).sockets, sid)
	} else {
		m.smux.Unlock()
		return fmt.Errorf("Sockets: attempted to remove socket of ID %v that doesn't exist", sid)
	}

	if forced {
		s.Close(2010, "Normal closure")
	} else {
		// User disconnected on their own. Poke the parent process to clean up.
		if m.conn.Listening() {
			cmd := BuildCommand(SOCKET_DISCONNECT, sid, m.conn)
			CmdQueue <- cmd
		}
	}

	return nil
}

func (m *Multiplexer) socketReceive(sid string, msg string) error {
	m.smux.Lock()
	defer m.smux.Unlock()

	if _, ok := m.sockets[sid]; ok {
		if m.conn.Listening() {
			cmd := BuildCommand(SOCKET_RECEIVE, sid+"\n"+msg, m.conn)
			CmdQueue <- cmd
		}
		return nil
	}

	return fmt.Errorf("Sockets: received a message for a socket of ID %v that does not exist: %v", sid, msg)
}

func (m *Multiplexer) socketSend(sid string, msg string) error {
	m.smux.Lock()
	defer m.smux.Unlock()

	if s, ok := m.sockets[sid]; ok && m.conn.Listening() {
		s.Send(msg)
		return nil
	}

	// This can happen occasionally on disconnect. Probably a race condition in
	// the parent process.
	return fmt.Errorf("Sockets: attempted to send to non-existent socket of ID %v: %v", sid, msg)
}

func (m *Multiplexer) channelAdd(cid string, sid string) error {
	m.cmux.Lock()
	defer m.cmux.Unlock()

	c, ok := m.channels[cid]
	if !ok {
		c = make(Channel)
		m.channels[cid] = c
	}

	c[sid] = DEFAULT_SUBCHANNEL_ID

	return nil
}

func (m *Multiplexer) channelRemove(cid string, sid string) error {
	m.cmux.Lock()
	defer m.cmux.Unlock()

	c, ok := m.channels[cid]
	if ok {
		if _, ok = c[sid]; !ok {
			return fmt.Errorf("Sockets: failed to remove nonexistent socket of ID %v from channel %v", sid, cid)
		}
	} else {
		// This occasionally happens on user disconnect.
		return nil
	}

	delete(c, sid)
	if len(c) == 0 {
		delete((*m).channels, cid)
	}

	return nil
}

func (m *Multiplexer) channelBroadcast(cid string, msg string) error {
	m.cmux.Lock()
	defer m.cmux.Unlock()

	c, ok := m.channels[cid]
	if !ok {
		// This happens occasionally when the last user leaves a room. Mitigate
		return nil
	}

	m.smux.Lock()
	defer m.smux.Unlock()

	for sid := range c {
		var s sockjs.Session
		if s, ok = m.sockets[sid]; ok {
			if m.conn.Listening() {
				s.Send(msg)
			}
		} else {
			delete(c, sid)
		}
	}

	return nil
}

func (m *Multiplexer) subchannelMove(cid string, scid byte, sid string) error {
	m.cmux.Lock()
	defer m.cmux.Unlock()

	c, ok := m.channels[cid]
	if !ok {
		return fmt.Errorf("Sockets: attempted to move socket of ID %v in channel %v, which does not exist, to subchannel %v", sid, cid, scid)
	}

	c[sid] = scid

	return nil
}

func (m *Multiplexer) subchannelBroadcast(cid string, msg string) error {
	m.cmux.Lock()
	defer m.cmux.Unlock()

	c, ok := m.channels[cid]
	if !ok {
		return fmt.Errorf("Sockets: attempted to broadcast to subchannels in channel %v, which doesn't exist: %v", cid, msg)
	}

	m.smux.Lock()
	defer m.smux.Unlock()

	msgs := make(map[byte]string)
	for sid, scid := range c {
		s, ok := m.sockets[sid]
		if !ok {
			return fmt.Errorf("Sockets: attempted to broadcast to subchannels in channel %v, but socket of ID %v doesn't exist: %v", cid, sid, msg)
		}

		if _, ok := msgs[scid]; !ok {
			switch scid {
			case DEFAULT_SUBCHANNEL_ID:
				msgs[scid] = m.scre.ReplaceAllString(msg, "$1")
			case P1_SUBCHANNEL_ID:
				msgs[scid] = m.scre.ReplaceAllString(msg, "$2")
			case P2_SUBCHANNEL_ID:
				msgs[scid] = m.scre.ReplaceAllString(msg, "$3")
			}
		}

		if m.conn.Listening() {
			s.Send(msgs[scid])
		}
	}

	return nil
}

// This is the HTTP handler for the SockJS server. This is where new sockets
// arrive for us to use.
func (m *Multiplexer) Handler(s sockjs.Session) {
	sid := m.socketAdd(s)
	for {
		if msg, err := s.Recv(); err == nil {
			if err = m.socketReceive(sid, msg); err != nil {
				// Likely a SockJS glitch if this happens at all.
				fmt.Printf("%v\n", err)
				break
			}
			continue
		}
		break
	}

	if err := m.socketRemove(sid, false); err != nil {
		// Socket was already removed by a message from the parent process.
		fmt.Printf("%v\n", err)
	}
}
