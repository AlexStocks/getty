package getty

import (
	"net"
	"strings"
	"context"
	"sync"
	"time"
)

// refers from https://github.com/facebookgo/grace/blob/master/gracenet/net.go#L180:6
func IsSameAddr(a1, a2 net.Addr) bool {
	if a1.Network() != a2.Network() {
		return false
	}
	a1s := a1.String()
	a2s := a2.String()
	if a1s == a2s {
		return true
	}

	// This allows for ipv6 vs ipv4 local addresses to compare as equal. This
	// scenario is common when listening on localhost.
	const ipv6prefix = "[::]"
	a1s = strings.TrimPrefix(a1s, ipv6prefix)
	a2s = strings.TrimPrefix(a2s, ipv6prefix)
	const ipv4prefix = "0.0.0.0"
	a1s = strings.TrimPrefix(a1s, ipv4prefix)
	a2s = strings.TrimPrefix(a2s, ipv4prefix)
	return a1s == a2s
}


var (
	defaultCtxKey int = 1
)

type Values struct {
	m map[interface{}]interface{}
}

func (v Values) Get(key interface{}) (interface{}, bool) {
	i, b := v.m[key]
	return i, b
}

func (c Values) Set(key interface{}, value interface{}) {
	c.m[key] = value
}

func (c Values) Delete(key interface{}) {
	delete(c.m, key)
}

type ValuesContext struct {
	context.Context
}

func NewValuesContext(ctx context.Context) *ValuesContext {
	if ctx == nil {
		ctx = context.Background()
	}

	return &ValuesContext{
		Context: context.WithValue(
			ctx,
			defaultCtxKey,
			Values{m: make(map[interface{}]interface{})},
		),
	}
}

func (c *ValuesContext) Get(key interface{}) (interface{}, bool) {
	return c.Context.Value(defaultCtxKey).(Values).Get(key)
}

func (c *ValuesContext) Delete(key interface{}) {
	c.Context.Value(defaultCtxKey).(Values).Delete(key)
}

func (c *ValuesContext) Set(key interface{}, value interface{}) {
	c.Context.Value(defaultCtxKey).(Values).Set(key, value)
}


type Wheel struct {
	sync.RWMutex
	span   time.Duration
	period time.Duration
	ticker *time.Ticker
	index  int
	ring   []chan struct{}
	once   sync.Once
	now    time.Time
}

func NewWheel(span time.Duration, buckets int) *Wheel {
	var (
		w *Wheel
	)

	if span == 0 {
		panic("@span == 0")
	}
	if buckets == 0 {
		panic("@bucket == 0")
	}

	w = &Wheel{
		span:   span,
		period: span * (time.Duration(buckets)),
		ticker: time.NewTicker(span),
		index:  0,
		ring:   make([](chan struct{}), buckets),
		now:    time.Now(),
	}

	go func() {
		var notify chan struct{}
		// var cw CountWatch
		// cw.Start()
		for t := range w.ticker.C {
			w.Lock()
			w.now = t

			// fmt.Println("index:", w.index, ", value:", w.bitmap.Get(w.index))
			notify = w.ring[w.index]
			w.ring[w.index] = nil
			w.index = (w.index + 1) % len(w.ring)

			w.Unlock()

			if notify != nil {
				close(notify)
			}
		}
		// fmt.Println("timer costs:", cw.Count()/1e9, "s")
	}()

	return w
}

func (w *Wheel) Stop() {
	w.once.Do(func() { w.ticker.Stop() })
}

func (w *Wheel) After(timeout time.Duration) <-chan struct{} {
	if timeout >= w.period {
		panic("@timeout over ring's life period")
	}

	var pos = int(timeout / w.span)
	if 0 < pos {
		pos--
	}

	w.Lock()
	pos = (w.index + pos) % len(w.ring)
	if w.ring[pos] == nil {
		w.ring[pos] = make(chan struct{})
	}
	// fmt.Println("pos:", pos)
	c := w.ring[pos]
	w.Unlock()

	return c
}

func (w *Wheel) Now() time.Time {
	w.RLock()
	now := w.now
	w.RUnlock()

	return now
}



type CountWatch struct {
	start time.Time
}

func (w *CountWatch) Start() {
	var t time.Time
	if t.Equal(w.start) {
		w.start = time.Now()
	}
}

func (w *CountWatch) Reset() {
	w.start = time.Now()
}

func (w *CountWatch) Count() int64 {
	return time.Since(w.start).Nanoseconds()
}
