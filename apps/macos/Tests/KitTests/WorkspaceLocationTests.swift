import Testing
@testable import Kit

struct WorkspaceLocationTests {
    @Test func homePathsUseTildeAtDirectoryBoundary() {
        #expect(WorkspaceLocation.homeRelative("/Users/person/Developer/kit", home: "/Users/person") == "~/Developer/kit")
        #expect(WorkspaceLocation.homeRelative("/Users/person", home: "/Users/person") == "~")
        #expect(WorkspaceLocation.homeRelative("/Users/person-two/kit", home: "/Users/person") == "/Users/person-two/kit")
        #expect(WorkspaceLocation.homeRelative("/tmp/kit", home: "/Users/person") == "/tmp/kit")
    }

    @Test func shorteningKeepsLastTwoSegments() {
        #expect(WorkspaceLocation.shortened("/Users/person/Developer/agent/kit-v2") == "…/agent/kit-v2")
        #expect(WorkspaceLocation.shortened("/repo") == "/repo")
        #expect(WorkspaceLocation.shortened("project/src") == "project/src")
    }
}
