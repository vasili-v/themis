package policy

import (
	"context"
	"time"

	"github.com/coredns/coredns/plugin/dnstap"
	tapmsg "github.com/coredns/coredns/plugin/dnstap/msg"
	"github.com/coredns/coredns/plugin/dnstap/taprw"
	tap "github.com/dnstap/golang-dnstap"
	"github.com/golang/protobuf/proto"
	pb "github.com/infobloxopen/themis/contrib/coredns/policy/dnstap"
	"github.com/miekg/dns"

	"github.com/infobloxopen/themis/pdp"
)

type dnstapSender interface {
	sendCRExtraMsg(w dns.ResponseWriter, msg *dns.Msg, attrs []*pb.DnstapAttribute)
}

type policyDnstapSender struct {
	ior dnstap.IORoutine
}

func newPolicyDnstapSender(io dnstap.IORoutine) dnstapSender {
	return &policyDnstapSender{ior: io}
}

// sendCRExtraMsg creates Client Response (CR) dnstap Message and writes an array
// of extra attributes to Dnstap.Extra field. Then it asynchronously sends the
// message with IORoutine interface
func (s *policyDnstapSender) sendCRExtraMsg(w dns.ResponseWriter, msg *dns.Msg, attrs []*pb.DnstapAttribute) {
	if w == nil || msg == nil {
		log.Errorf("Failed to create dnstap CR message - no DNS response message found")
		return
	}

	now := time.Now()
	b := tapmsg.New().Time(now).Addr(w.RemoteAddr())
	b.Msg(msg)
	crMsg, err := b.ToClientResponse()
	if err != nil {
		log.Errorf("Failed to create dnstap CR message (%v)", err)
		return
	}

	timeNs := uint32(now.Nanosecond())
	crMsg.ResponseTimeNsec = &timeNs
	t := tap.Dnstap_MESSAGE

	var extra []byte
	if len(attrs) > 0 {
		extra, err = proto.Marshal(&pb.Extra{Attrs: attrs})
		if err != nil {
			log.Errorf("Failed to create extra data for dnstap CR message (%v)", err)
		}
	}
	dnstapMsg := tap.Dnstap{Type: &t, Message: crMsg, Extra: extra}
	s.ior.Dnstap(dnstapMsg)
}

func resetCqCr(ctx context.Context) {
	if v := ctx.Value(dnstap.DnstapSendOption); v != nil {
		if so, ok := v.(*taprw.SendOption); ok {
			so.Cq = false
			so.Cr = false
		}
	}
}

func newDnstapAttribute(a pdp.AttributeAssignment) *pb.DnstapAttribute {
	return &pb.DnstapAttribute{
		Id:    a.GetID(),
		Value: serializeOrPanic(a),
	}
}
