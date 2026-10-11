package toolpresentation

import (
	"encoding/json"
	"strings"
)

// OpensAgent returns the subagent a subagent call addresses, whose
// conversation its row's summary opens, or empty when it addresses none.
func OpensAgent(call Call) string {
	if strings.ToLower(call.Name) != "subagent" || call.ArgumentsTruncated {
		return ""
	}
	var args struct {
		Agent string `json:"agent"`
	}
	if json.Unmarshal([]byte(call.Arguments), &args) != nil {
		return ""
	}
	return strings.TrimSpace(args.Agent)
}
