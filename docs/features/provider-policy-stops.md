# Provider policy stops

Kit recognizes OpenAI's exact `misalignment_policy_violation` error code, not
message text. It handles HTTP failures before streaming, streamed error events,
and failed Responses objects after output has begun. This implements the local
request handling described in OpenAI's
[misalignment monitoring guide](https://developers.openai.com/api/docs/guides/safety-checks/misalignment-monitoring).

The current turn fails through the ordinary provider-error presentation:
`Provider stopped this request (misalignment_policy_violation)`. Kit does not
retry the response, execute tool calls from its partial output, or automatically
start queued follow-up prompts or automatic naming from that settlement. A
concurrent pause does not make the stopped turn resumable. Requests made through
this Responses adapter disable SDK-level retries; ordinary transient failures
still use Kit's bounded retry policy.

Queued user inputs remain available through the existing restore/edit flow.
There is no persistent local conversation/session lock, unlock command, special
recovery screen, or automatic fork. After restoring any pending inputs as usual,
a user can submit another prompt normally; the provider may accept it or return
another error. Other sessions are unaffected.

The diagnostic assistant record retains the policy code, available HTTP request
ID and response ID, and completed output items received before the stop. Prior
completed tool actions and results remain recorded. A provider stop does not
undo actions, cancel independent sessions or supervised children, or establish
that external side effects have been reversed. Tools in the current synchronous
model/tool loop are dispatched only after a successful model response; partial
calls from a failed response are diagnostic records, not actions to execute.

This does not subscribe to project safety-alert webhooks or infer the outcome of
an external task from an alert. Broader async-tool behavior is tracked by
`CORE-GPT6-005` in the [core backlog](../../backlog/core.md).
