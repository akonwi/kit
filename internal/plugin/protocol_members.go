package plugin

import (
	"reflect"
	"strings"

	"github.com/akonwi/kit/api/pluginprotocol"
)

// membersOf lists the JSON member names of a plugin-sent protocol record, so
// the host's strict parsers accept exactly what the published types declare.
func membersOf[T any]() []string {
	typ := reflect.TypeFor[T]()
	members := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			members = append(members, name)
		}
	}
	return members
}

// Members each params object may contain.
var (
	commandRegisterMembers  = membersOf[pluginprotocol.CommandRegisterParams]()
	localIDMembers          = membersOf[pluginprotocol.LocalIDParams]()
	toolRegisterMembers     = membersOf[pluginprotocol.ToolRegisterParams]()
	toolIDMembers           = membersOf[pluginprotocol.ToolIDParams]()
	subagentRegisterMembers = membersOf[pluginprotocol.SubagentRegisterParams]()
	footerSetMembers        = membersOf[pluginprotocol.FooterSetParams]()
	footerSegmentMembers    = membersOf[pluginprotocol.FooterSegment]()
	footerStyleMembers      = membersOf[pluginprotocol.FooterStyle]()
	footerTargetMembers     = membersOf[pluginprotocol.FooterTargetParams]()
	toastMembers            = membersOf[pluginprotocol.ToastParams]()
	confirmMembers          = membersOf[pluginprotocol.ConfirmParams]()
	inputMembers            = membersOf[pluginprotocol.InputParams]()
	selectMembers           = membersOf[pluginprotocol.SelectParams]()
	selectOptionMembers     = membersOf[pluginprotocol.SelectOption]()
	submitMessageMembers    = membersOf[pluginprotocol.SubmitMessageParams]()
	openURLMembers          = membersOf[pluginprotocol.OpenURLParams]()
	decisionMembers         = membersOf[pluginprotocol.ToolCallDecision]()
	toolResultMembers       = membersOf[pluginprotocol.ToolExecuteResult]()
)
