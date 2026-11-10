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

// Members each params object may contain. Version 1 additionally accepts the
// legacy members listed with it; version 2 does not have them.
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

	// Version 1 footer items could name a side, a click callback, or a URL
	// action; Kit rejects any but the right side when they are used.
	v1FooterSetExtras = []string{"side", "clickable", "action"}
	// Version 1 select requests could ask for filtering, which Kit ignores.
	v1SelectExtras = []string{"filterable"}
)

// versionMembers returns the members of a params object in a protocol
// version: version 2 exactly as declared; version 1 without parentId, which
// it does not have, and with its legacy extras.
func versionMembers(version int, members []string, v1Extras ...string) []string {
	if version >= 2 {
		return members
	}
	result := make([]string, 0, len(members)+len(v1Extras))
	for _, member := range members {
		if member != "parentId" {
			result = append(result, member)
		}
	}
	return append(result, v1Extras...)
}
