package rpc

import (
	"sync"
	"time"
)

import (
	jerrors "github.com/juju/errors"
)

import (
	"github.com/AlexStocks/getty/transport"
)

/////////////////////////////////////////////////
// sender
/////////////////////////////////////////////////

type sender struct {
	pendingLock      sync.Mutex
	pendingResponses map[SequenceType]*PendingResponse
}

func newSender() *sender {
	return &sender{
		pendingLock:      sync.Mutex{},
		pendingResponses: make(map[SequenceType]*PendingResponse, 32),
	}
}

func (s *sender) addPendingResponse(pr *PendingResponse) {
	s.pendingLock.Lock()
	defer s.pendingLock.Unlock()
	s.pendingResponses[pr.seq] = pr
}

func (s *sender) removePendingResponse(seq SequenceType) *PendingResponse {
	s.pendingLock.Lock()
	defer s.pendingLock.Unlock()
	if s.pendingResponses == nil {
		return nil
	}
	if presp, ok := s.pendingResponses[seq]; ok {
		delete(s.pendingResponses, seq)
		return presp
	}
	return nil
}

func (s *sender) transfer(session getty.Session, pkg *GettyPackage, rsp *PendingResponse, opts CallOptions) error {
	var (
		err      error
		sequence SequenceType
	)

	sequence = pkg.H.Sequence
	// cond1
	if rsp != nil {
		rsp.seq = sequence
		s.addPendingResponse(rsp)
	}

	err = session.WritePkg(pkg, opts.RequestTimeout)
	if err != nil {
		s.removePendingResponse(rsp.seq)
	} else if rsp != nil { // cond2
		// cond2 should not merged with cond1. cause the response package may be returned very
		// soon and it will be handled by other goroutine.
		rsp.readStart = time.Now()
	}

	return jerrors.Trace(err)
}

func (s *sender) call(ss getty.Session, ct CallType, pkg *mq.Packet,
	reply interface{}, callback AsyncCallback, opts CallOptions) error {
	var rsp *PendingResponse
	if ct != CT_OneWay {
		rsp = NewPendingResponse()
		rsp.callback = callback
		rsp.opts = opts
	}

	if err := s.transfer(ss, pkg, rsp, opts); err != nil {
		return jerrors.Trace(err)
	}

	if ct == CT_OneWay || callback != nil {
		return nil
	}

	select {
	case <-getty.GetTimeWheel().After(opts.ResponseTimeout):
		// do not close connection when can not get response
		s.removePendingResponse(rsp.seq)
		return jerrors.Trace(errClientReadTimeout)

	case <-rsp.done:
		if reply != nil && rsp.reply != nil {
			replyPkg, ok1 := reply.(*mq.Packet)
			rspPkg, ok2 := rsp.reply.(*mq.Packet)
			if ok1 && ok2 {
				*replyPkg = *rspPkg
			}
		}
	}

	return nil
}
