import Testing
@testable import Kit

struct SessionContextTests {
    @Test func percentagesAndLevelsMatchTUI() throws {
        for (tokens, percentage, level) in [
            (1, 1, SessionContext.Level.normal),
            (410, 41, .normal), (794, 79, .normal), (795, 80, .warning),
            (900, 90, .warning), (905, 91, .critical), (1200, 100, .critical)
        ] {
            let context = try #require(SessionContext(tokens: tokens, capacity: 1000))
            #expect(context.percentage == percentage)
            #expect(context.level == level)
        }
        #expect(SessionContext(tokens: 0, capacity: 1000) == nil)
        #expect(SessionContext(tokens: 100, capacity: nil) == nil)
    }

    @Test func importedProgressColorsAreUsed() throws {
        let theme = MicaTheme(dark: false, overrides: [
            "progressNormal": "#123456", "progressWarning": "#abcdef", "progressCritical": "#654321"
        ])
        for (tokens, token) in [(40, "progressNormal"), (80, "progressWarning"), (95, "progressCritical")] {
            let context = try #require(SessionContext(tokens: tokens, capacity: 100))
            #expect(context.color(in: theme) == theme.token(token, fallback: theme.text))
        }
    }
}
