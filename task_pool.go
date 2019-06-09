package getty

import (
	"fmt"
	"sync"
	"sync/atomic"
)

const (
	defaultTaskQNumber = 10
	defaultTaskQLen    = 128
)

// task t
type task struct {
	session *session
	pkg     interface{}
}

type taskPoolOptions struct {
	tQLen      int32 // task queue length
	tQNumber   int32 // task queue number
	tQPoolSize int32 // task pool size
}

func (o *taskPoolOptions) Validate() {
	if o.tQPoolSize == 0 {
		panic(fmt.Sprintf("[getty][task_pool] illegal pool size %d", o.tQPoolSize))
	}

	if o.tQLen == 0 {
		o.tQLen = defaultTaskQLen
	}

	if o.tQNumber < 1 {
		o.tQNumber = defaultTaskQNumber
	}

	if o.tQNumber > o.tQPoolSize {
		o.tQNumber = o.tQPoolSize
	}
}

// task pool: manage task ts
type taskPool struct {
	taskPoolOptions

	idx    uint32 // round robin index
	qArray []chan task
	wg     sync.WaitGroup

	once sync.Once
	done chan struct{}
}

// build a task pool
func newTaskPool(opts taskPoolOptions) *taskPool {
	opts.Validate()

	p := &taskPool{
		taskPoolOptions: opts,
		qArray:          make([]chan task, opts.tQNumber),
		done:            make(chan struct{}),
	}

	for i := int32(0); i < p.tQNumber; i++ {
		p.qArray[i] = make(chan task, p.tQLen)
	}

	return p
}

// start task pool
func (p *taskPool) start() {
	for i := int32(0); i < p.tQPoolSize; i++ {
		p.wg.Add(1)
		workerID := i
		q := p.qArray[workerID%p.tQNumber]
		go p.run(int(workerID), q)
	}
}

// worker
func (p *taskPool) run(id int, q chan task) {
	defer p.wg.Done()

	var (
		ok bool
		t  task
	)

	for {
		select {
		case <-p.done:
			if 0 < len(q) {
				log.Warn("[getty][task_pool] task worker %d exit now while its task buffer length %d is greater than 0",
					id, len(q))
			} else {
				log.Info("[getty][task_pool] task worker %d exit now", id)
			}
			return

		case t, ok = <-q:
			if ok {
				t.session.listener.OnMessage(t.session, t.pkg)
			}
		}
	}
}

// add task
func (p *taskPool) AddTask(t task) {
	id := atomic.AddUint32(&p.idx, 1) % defaultTaskQNumber

	select {
	case <-p.done:
		return
	case p.qArray[id] <- t:
	}
}

// stop all tasks
func (p *taskPool) stop() {
	select {
	case <-p.done:
		return
	default:
		p.once.Do(func() {
			close(p.done)
		})
	}
}

// check whether the session has been closed.
func (p *taskPool) isClosed() bool {
	select {
	case <-p.done:
		return true

	default:
		return false
	}
}

func (p *taskPool) close() {
	p.stop()
	p.wg.Wait()
	for i := range p.qArray {
		close(p.qArray[i])
	}
}
