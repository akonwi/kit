package subagent_test

import (
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/subagent"
	"github.com/akonwi/kit/internal/systemprompt"
)

func TestCatalogSectionPresentsNamesAndDescriptionsWithoutModels(t *testing.T) {
	catalog, err := subagent.NewCatalog(
		subagent.Definition{
			Name: "reviewer", Description: "Reviews <changes> & risks.", Model: "provider/private-model", Instructions: "Review.",
			Source: subagent.Source{Kind: subagent.SourceProject, Path: "/repo/.kit/agents/reviewer.md"},
		},
		subagent.Definition{
			Name: "scout", Description: "Maps the codebase.", Instructions: "Inspect.",
			Source: subagent.Source{Kind: subagent.SourceUser, Path: "/home/user/.kit/agents/scout.md"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	section, err := catalog.CatalogSection()
	if err != nil {
		t.Fatal(err)
	}
	want := `The following named subagents are available for delegated work.
Use the subagent tool when a task matches an agent's specialization. Starting work is asynchronous; continue useful parent work instead of immediately waiting unless the result is required.

<available_subagents>
  <subagent>
    <name>reviewer</name>
    <description>Reviews &lt;changes&gt; &amp; risks.</description>
  </subagent>
  <subagent>
    <name>scout</name>
    <description>Maps the codebase.</description>
  </subagent>
</available_subagents>`
	if section.ID != "kit.subagents.catalog" || section.Kind != systemprompt.SectionFeature || section.Text != want {
		t.Fatalf("CatalogSection() = %#v\nwant text:\n%s", section, want)
	}
	if strings.Contains(section.Text, "private-model") || len(section.Sources) != 2 || section.Sources[0].ID != "subagent:reviewer" {
		t.Fatalf("catalog leaks model or has incorrect sources: %#v", section)
	}
}

func TestEmptyCatalogSectionHasNoPromptText(t *testing.T) {
	catalog, err := subagent.NewCatalog()
	if err != nil {
		t.Fatal(err)
	}
	section, err := catalog.CatalogSection()
	if err != nil {
		t.Fatal(err)
	}
	if section.ID != "kit.subagents.catalog" || section.Text != "" || len(section.Sources) != 0 {
		t.Fatalf("CatalogSection() = %#v", section)
	}
}
