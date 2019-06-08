package getty

import (
	"fmt"
	"sync"
	"sync/atomic"
)

import (
	log "github.com/AlexStocks/log4go"
)

//func init() {
//	rand.Seed(time.Now().UnixNano())
//}
//
//var (
//	// The golang rand generators are *not* intrinsically thread-safe.
//	randIDLock sync.Mutex
//	randIDGen  = rand.New(rand.NewSource(time.Now().UnixNano()))
//)
//
//func randID() uint64 {
//	randIDLock.Lock()
//	defer randIDLock.Unlock()
//
//	return uint64(randIDGen.Int63())
//}

// task t
type task struct {
	session *session
	pkg     interface{}
}

// task pool: manage task ts
type taskPool struct {
	idx    uint32
	qLen   int32 // task queue length
	size   int32 // task queue pool size
	qArray []chan task
	wg     sync.WaitGroup

	once sync.Once
	done chan struct{}
}

// build a task pool
func newTaskPool(poolSize int32, taskQLen int32) *taskPool {
	return &taskPool{
		size:   poolSize,
		qLen:   taskQLen,
		qArray: make([]chan task, poolSize),
		done:   make(chan struct{}),
	}
}

// start task pool
func (p *taskPool) start() {
	if p.size == 0 {
		panic(fmt.Sprintf("[getty][task_pool] illegal pool size %d", p.size))
	}

	if p.qLen == 0 {
		panic(fmt.Sprintf("[getty][task_pool] illegal t queue length %d", p.qLen))
	}

	for i := int32(0); i < p.size; i++ {
		p.qArray[i] = make(chan task, p.qLen)
		p.wg.Add(1)
		taskID := i
		p.run(int(taskID))
	}
}

// task
func (p *taskPool) run(id int) {
	defer p.wg.Done()

	var (
		ok bool
		t  task
	)

	for {
		select {
		case <-p.done:
			log.Warn("[getty][task_pool] task %d exit now while its task length is %d",
				id, len(p.qArray[id]))
			return

		case t, ok = <-p.qArray[id]:
			if ok {
				t.session.listener.OnMessage(t.session, t.pkg)
			}
		}
	}
}

// add task
func (p *taskPool) AddTask(t task) {
	//id := randID() % uint64(p.size)
	id := atomic.AddUint32(&p.idx, 1) % uint32(p.size)

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
