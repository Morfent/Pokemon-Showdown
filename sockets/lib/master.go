package sockets

var CmdQueue = make(chan Command)

type master struct {
	wpool chan chan Command
	count int
}

func NewMaster(count int) *master {
	wpool := make(chan chan Command, count)
	return &master{
		wpool: wpool,
		count: count}
}

func (m *master) Spawn() {
	for i := 0; i < m.count; i++ {
		w := newWorker(m.wpool)
		w.listen()
	}
}

func (m *master) Listen() {
	for {
		cmd := <-CmdQueue
		cmdch := <-m.wpool
		cmdch <- cmd
	}
}

type worker struct {
	wpool chan chan Command
	cmdch chan Command
	quit  chan bool
}

func newWorker(wpool chan chan Command) *worker {
	cmdch := make(chan Command)
	quit := make(chan bool)
	return &worker{
		wpool: wpool,
		cmdch: cmdch,
		quit:  quit}
}

func (w *worker) listen() {
	go func() {
		for {
			w.wpool <- w.cmdch
			select {
			case cmd := <-w.cmdch:
				cmd.target.Process(cmd)
			case <-w.quit:
				return
			}
		}
	}()
}
