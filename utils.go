package getty

import (
	"context"
	"net"
	"strings"
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
