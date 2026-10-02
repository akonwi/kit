package httpapi

// Catalog returns the currently migrated session operations in stable order.
func Catalog() []Descriptor {
	return []Descriptor{
		ListSessions.Describe(), CreateSession.Describe(), GetSession.Describe(), RenameSession.Describe(), DeleteSession.Describe(), DisposeTemporarySession.Describe(), ForkSession.Describe(), ChangeSessionCWD.Describe(), ConfigureSession.Describe(), CompactSession.Describe(), ReloadSession.Describe(),
		GetScratchpad.Describe(), UpdateScratchpad.Describe(),
		ListDiffTargets.Describe(), ObserveDiff.Describe(), ObserveWorkingTree.Describe(), ReadFileDiff.Describe(),
		ListAnnotations.Describe(), CreateAnnotation.Describe(), UpdateAnnotation.Describe(), DeleteAnnotation.Describe(),
		GetBashHistory.Describe(), StartBash.Describe(), GetBash.Describe(), AbortBash.Describe(),
		GetSessionVCS.Describe(), StreamSessionVCS.Describe(),
		ExecutePluginCommand.Describe(), StreamPluginToasts.Describe(),
		SubmitPrompt.Describe(), StartPrompt.Describe(), StartPromptCommand.Describe(), SubmitPromptCommand.Describe(), Prompt.Describe(),
		RestoreTurnFollowUps.Describe(), PromoteTurnFollowUps.Describe(),
		GetTurn.Describe(), AbortTurn.Describe(), RespondInteraction.Describe(),
		GetMessagePage.Describe(), GetTranscriptPage.Describe(), GetSessionEventPage.Describe(), StreamSessionEvents.Describe(),
	}
}
