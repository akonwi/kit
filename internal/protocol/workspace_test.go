package protocol

import "testing"

func TestValidateWorkspacePath(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"file.go", "internal/workspace/service.go", ".hidden"} {
		if err := ValidateWorkspacePath(value, false); err != nil {
			t.Errorf("%q: %v", value, err)
		}
	}
	for _, value := range []string{"", ".", "../secret", "/absolute", "a//b", "a/", `a\b`, "bad\nname", "bad\u202ename"} {
		if err := ValidateWorkspacePath(value, false); err == nil {
			t.Errorf("%q validated", value)
		}
	}
	if err := ValidateWorkspacePath("", true); err != nil {
		t.Fatalf("root path: %v", err)
	}
}

func TestWorkspaceErrorsEnforceDetailSchemas(t *testing.T) {
	t.Parallel()
	validID := "workspace_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	valid := WorkspaceError{Code: WorkspaceErrorStaleWorkspace, Message: "stale", Details: map[string]string{"currentWorkspaceId": validID, "currentWorkspaceState": "ready"}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, details := range []map[string]string{{"currentWorkspaceId": "workspace_bad", "currentWorkspaceState": "ready"}, {"currentWorkspaceId": validID, "currentWorkspaceState": "unknown"}} {
		candidate := valid
		candidate.Details = details
		if candidate.Validate() == nil {
			t.Fatalf("invalid details validated: %#v", details)
		}
	}
	limit := WorkspaceError{Code: WorkspaceErrorLimit, Message: "limit", Details: map[string]string{"limit": "unknown"}}
	if limit.Validate() == nil {
		t.Fatal("unknown limit validated")
	}
}

func TestDirectoryOmissionsMustBeUniqueAndCanonical(t *testing.T) {
	t.Parallel()
	workspace := WorkspaceRef{SessionID: "session_test", CWD: "/workspace", WorkspaceID: "workspace_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", State: WorkspaceReady, Limits: DefaultWorkspaceLimits()}
	base := DirectoryPage{SessionID: "session_test", Workspace: workspace, Revision: "directory_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", Entries: []WorkspaceDirectoryEntry{}, Omissions: []WorkspaceOmission{{Reason: "unsupported_name", Count: 1}, {Reason: "entry_raced", Count: 1}}}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	base.Omissions = []WorkspaceOmission{{Reason: "entry_raced", Count: 1}, {Reason: "unsupported_name", Count: 1}}
	if base.Validate() == nil {
		t.Fatal("out-of-order omissions validated")
	}
	base.Omissions = []WorkspaceOmission{{Reason: "entry_raced", Count: 1}, {Reason: "entry_raced", Count: 2}}
	if base.Validate() == nil {
		t.Fatal("duplicate omissions validated")
	}
}

func TestWorkspaceFileReadValidate(t *testing.T) {
	t.Parallel()
	valid := WorkspaceFileRead{SessionID: "session_test", Workspace: WorkspaceRef{SessionID: "session_test", CWD: "/workspace", WorkspaceID: "workspace_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", State: WorkspaceReady, Limits: DefaultWorkspaceLimits()}, Path: "file.txt", Revision: "file_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", Encoding: "utf-8", Content: "hello", ReturnedBytes: 5, ReturnedLines: 1}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	valid.ReturnedBytes = 4
	if err := valid.Validate(); err == nil {
		t.Fatal("mismatched byte count validated")
	}
}

func TestWorkspaceRequestConcurrencyLimitsAreBoundedAndAllowNoPendingQueue(t *testing.T) {
	t.Parallel()
	workspace := WorkspaceRef{SessionID: "session_test", CWD: "/workspace", WorkspaceID: "workspace_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", State: WorkspaceReady, Limits: DefaultWorkspaceLimits()}
	workspace.Limits.MaxPendingRequests = 0
	if err := workspace.Validate(); err != nil {
		t.Fatalf("zero pending requests rejected: %v", err)
	}
	workspace.Limits.MaxActiveRequests = MaxWorkspaceActiveRequests + 1
	if workspace.Validate() == nil {
		t.Fatal("excess active requests validated")
	}
	workspace.Limits = DefaultWorkspaceLimits()
	workspace.Limits.MaxPendingRequests = MaxWorkspacePendingRequests + 1
	if workspace.Validate() == nil {
		t.Fatal("excess pending requests validated")
	}
}
