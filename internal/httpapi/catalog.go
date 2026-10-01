package httpapi

// Catalog returns the currently migrated session operations in stable order.
func Catalog() []Descriptor {
	return []Descriptor{
		GetScratchpad.Describe(), UpdateScratchpad.Describe(),
		GetSessionVCS.Describe(), StreamSessionVCS.Describe(),
		ExecutePluginCommand.Describe(), StreamPluginToasts.Describe(),
		SubmitPrompt.Describe(), StartPrompt.Describe(), StartPromptCommand.Describe(), Prompt.Describe(),
		RestoreTurnFollowUps.Describe(), PromoteTurnFollowUps.Describe(),
		GetTurn.Describe(), AbortTurn.Describe(), RespondInteraction.Describe(),
		GetMessagePage.Describe(), GetTranscriptPage.Describe(), GetSessionEventPage.Describe(), StreamSessionEvents.Describe(),
	}
}
