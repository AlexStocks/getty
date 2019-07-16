/******************************************************
# MAINTAINER : wongoo
# LICENCE    : Apache License 2.0
# EMAIL      : gelnyang@163.com
# MOD        : 2019-06-11
******************************************************/

package hello

import (
	"github.com/dubbogo/getty"

	"time"
)

type MessageHandler struct {
	SessionOnOpen func(session getty.Session)
}

func (h *MessageHandler) OnOpen(session getty.Session) error {
	log.Infof("OnOpen session{%s} open", session.Stat())
	if h.SessionOnOpen != nil {
		h.SessionOnOpen(session)
	}
	return nil
}

func (h *MessageHandler) OnError(session getty.Session, err error) {
	log.Infof("OnError session{%s} got error{%v}, will be closed.", session.Stat(), err)
}

func (h *MessageHandler) OnClose(session getty.Session) {
	log.Infof("OnClose session{%s} is closing......", session.Stat())
}

func (h *MessageHandler) OnMessage(session getty.Session, pkg interface{}) {
	s, ok := pkg.(string)
	if !ok {
		log.Infof("illegal package{%#v}", pkg)
		return
	}

	log.Infof("OnMessage: %s", s)
}

func (h *MessageHandler) OnCron(session getty.Session) {
	active := session.GetActive()
	if CronPeriod.Nanoseconds() < time.Since(active).Nanoseconds() {
		log.Infof("OnCorn session{%s} timeout{%s}", session.Stat(), time.Since(active).String())
		session.Close()
	}
}
