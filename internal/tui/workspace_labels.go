package tui

func workspacePaneLabels(snapshot shellSnapshot, panes []workspacePaneDescriptor) []string {
	labels := make([]string, len(panes))
	counts := make(map[string]int, len(panes))
	for index, descriptor := range panes {
		definition, ok := workspacePaneDefinitions[descriptor.Kind]
		if !ok {
			continue
		}
		labels[index] = definition.Label(snapshot, descriptor)
		counts[labels[index]]++
	}
	for index, descriptor := range panes {
		if descriptor.Kind != workspacePaneFile || counts[labels[index]] < 2 {
			continue
		}
		labels[index] = descriptor.Path
	}
	pathCounts := make(map[string]int, len(panes))
	for _, label := range labels {
		pathCounts[label]++
	}
	for index, descriptor := range panes {
		if descriptor.Kind != workspacePaneFile || pathCounts[labels[index]] < 2 {
			continue
		}
		workspace := descriptor.WorkspaceID
		if len(workspace) > 8 {
			workspace = workspace[len(workspace)-8:]
		}
		labels[index] = labels[index] + " · " + workspace
	}
	return labels
}
