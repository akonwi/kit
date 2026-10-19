package subagent

import "testing"

func TestDeliverToInboxDescribesRequestsRepliesAndFailures(t *testing.T) {
	t.Parallel()
	request := Request{ID: "subrequest_1", SenderName: "reviewer", RecipientName: "tester", Message: "Is shell exit covered?", Reply: "Yes."}
	cases := []struct {
		name    string
		request Request
		origin  TaskOrigin
		want    InboxDelivery
	}{
		{"request", request, TaskOriginRequest, InboxDelivery{
			Text: "Subagent request subrequest_1 from reviewer:\nIs shell exit covered?\nUse subagent_reply with this receipt to answer.",
			From: "reviewer", Message: "Is shell exit covered?",
		}},
		{"reply", withState(request, RequestReplied, ""), TaskOriginReply, InboxDelivery{
			Text: "Subagent reply to subrequest_1 from tester:\nYes.", From: "tester", Message: "Yes.",
		}},
		{"failure", withState(request, RequestExpired, "request expired"), TaskOriginReply, InboxDelivery{
			Text: "Subagent request subrequest_1 failed: request expired", From: "tester", Failure: "request expired",
		}},
	}
	for _, test := range cases {
		if got := DeliverToInbox(test.request, test.origin); got != test.want {
			t.Errorf("%s: DeliverToInbox = %#v, want %#v", test.name, got, test.want)
		}
	}
}

func withState(request Request, state RequestState, failure string) Request {
	request.State, request.Failure = state, failure
	return request
}
