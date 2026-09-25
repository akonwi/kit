package protocol

import "fmt"

func validPluginInteractionOwner(owner PluginInteractionOwner) bool {
	return pluginCommandPluginID.MatchString(owner.PluginID) && pluginCommandOwner.MatchString(owner.Instance)
}

func validateInteractionPresentation(request InteractionRequest) error {
	if !validInteractionText(request.ConfirmLabel, 128, false) || !validInteractionText(request.CancelLabel, 128, false) || !validInteractionText(request.Placeholder, MaxInteractionTitleBytes, false) || !validInteractionText(request.InitialValue, MaxInteractionAnswerBytes, false) {
		return fmt.Errorf("invalid interaction presentation")
	}
	if request.Kind != InteractionConfirm && (request.ConfirmLabel != "" || request.CancelLabel != "" || request.DefaultValue != nil) {
		return fmt.Errorf("confirm presentation on other interaction")
	}
	if request.Kind != InteractionInput && request.InitialValue != "" {
		return fmt.Errorf("initial value on other interaction")
	}
	if request.Kind != InteractionSelect && request.Filterable != nil {
		return fmt.Errorf("filtering on other interaction")
	}
	if request.Kind != InteractionSelect && request.Kind != InteractionInput && request.Placeholder != "" {
		return fmt.Errorf("placeholder on other interaction")
	}
	return nil
}
