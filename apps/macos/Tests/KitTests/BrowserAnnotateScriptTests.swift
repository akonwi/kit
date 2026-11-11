import AppKit
import Foundation
import Testing
import WebKit
@testable import Kit

/// Runs Kit's annotation page script in WebKit against a served fixture
/// page, with a frame from another origin.
@MainActor @Suite(.serialized) struct BrowserAnnotateScriptTests {
    static let page = """
        <!doctype html><html><head><title>Pricing — Acme</title><style>
        body { margin: 0; font: 14px Helvetica; }
        .top { position: absolute; top: 0; left: 0; width: 800px; height: 60px; }
        h1 { margin: 0; }
        main { position: absolute; top: 60px; left: 0; width: 800px; height: 540px; }
        .plans { position: absolute; top: 40px; left: 0; width: 800px; height: 200px; display: flex; }
        .plan { width: 260px; height: 200px; margin-right: 10px; flex: none; }
        h3 { margin: 0; height: 30px; }
        .cta { display: block; width: 120px; height: 30px; margin: 20px; }
        .login { position: absolute; top: 260px; left: 0; width: 400px; height: 200px; }
        x-card { position: absolute; top: 260px; left: 420px; width: 200px; height: 80px; display: block; }
        .framed { position: absolute; top: 360px; left: 420px; width: 160px; height: 80px; border: 0; }
        .foreign { position: absolute; top: 360px; left: 600px; width: 150px; height: 80px; border: 0; }
        .long { position: absolute; top: 460px; left: 420px; width: 300px; height: 60px; overflow: hidden; }
        </style></head><body>
        <div id="root"><div class="app">
          <header class="top"><h1>Simple pricing</h1></header>
          <main>
            <div class="plans">
              <div class="plan" data-inspector-relative-path="src/pages/Pricing.tsx" data-inspector-line="40"><h3>Free</h3>
                <a class="cta" href="/checkout?plan=free&amp;token=abc123secret" data-testid="free-cta">Get started</a></div>
              <div class="plan pro" data-inspector-relative-path="src/pages/Pricing.tsx" data-inspector-line="42"><h3>Pro</h3>
                <a class="cta" id="buy" href="/checkout?plan=pro" aria-label="Upgrade to the Pro plan">Upgrade to Pro</a>
                <p class="price"><a href="/faq"><b>$12</b>/mo</a> billed monthly</p></div>
              <div class="plan"><h3>Team</h3><a class="cta" href="/contact">Contact sales</a></div>
            </div>
            <form class="login" action="/session?csrf=form-SECRET">
              <input name="email" value="grace@example.com">
              <input type="password" name="pass" value="hunter2">
              <input type="hidden" name="csrf_token" value="tok-SECRET">
              <textarea name="note">draft-SECRET</textarea>
              <div contenteditable="true">editable-SECRET</div>
              <select name="plan"><option value="pro" selected>Pro</option></select>
              <button type="submit" nonce="nonce-SECRET">Sign in</button>
            </form>
            <x-card></x-card>
            <iframe class="framed" srcdoc="<body style='margin:0'><p id='inside' style='margin:0;height:40px'>Framed</p></body>"></iframe>
            <iframe class="foreign"></iframe>
            <div class="long" \(Self.manyAttributes)>\(String(repeating: "Lorem ipsum dolor sit amet. ", count: 60))</div>
          </main>
        </div></div>
        <script>
          // Another host on the same server is another origin.
          document.querySelector(".foreign").src = "http://localhost:" + location.port + "/frame";
          document.cookie = "sid=cookie-SECRET";
          localStorage.setItem("token", "storage-SECRET");
          document.querySelector("[name=email]").value = "typed-SECRET";
          const host = document.querySelector("x-card");
          host.attachShadow({ mode: "open" }).innerHTML = "<button class='inner' style='width:100px;height:30px'>Shadow</button>";
        </script>
        </body></html>
        """
    static let manyAttributes = (1...20).map { "data-field-\($0)=\"value \($0)\"" }.joined(separator: " ")

    /// Keeps the page in a window, laid out at 800 by 600.
    let window = NSWindow(testContentRect: NSRect(x: 0, y: 0, width: 800, height: 600), styleMask: [.titled])

    private func loaded() async throws -> (engine: any BrowserEngine, server: LoopbackHTTPServer) {
        let server = try LoopbackHTTPServer(routes: ["/": .init(body: Self.page), "/frame": .init(body: "<p>Elsewhere</p>")])
        let url = try await server.start()
        let engine = WebKitBrowserEngine.make()
        window.contentView = engine.view
        engine.load(url)
        try await eventually { !engine.state.loading && engine.state.title == "Pricing — Acme" }
        return (engine, server)
    }

    private func run(_ engine: any BrowserEngine, _ operation: String, _ arguments: [String: Any] = [:]) async throws -> [String: Any] {
        let json = try await engine.runAnnotationScript(operation, arguments: arguments)
        return try #require(try JSONSerialization.jsonObject(with: Data(json.utf8)) as? [String: Any])
    }

    private func state(_ engine: any BrowserEngine, _ operation: String, _ arguments: [String: Any] = [:]) async throws -> BrowserAnnotationPageState {
        let json = try await engine.runAnnotationScript(operation, arguments: arguments)
        return try JSONDecoder().decode(BrowserAnnotationPageState.self, from: Data(json.utf8))
    }

    private func capture(_ engine: any BrowserEngine) async throws -> (BrowserElementCapture, String) {
        let json = try await engine.runAnnotationScript("capture", arguments: [:])
        return (try JSONDecoder().decode(BrowserElementCapture.self, from: Data(json.utf8)), json)
    }

    /// Evaluates `script` in the page's own world.
    private func page(_ engine: any BrowserEngine, _ script: String) async throws -> Any? {
        let view = try #require(engine.view as? WKWebView)
        return try await view.evaluateJavaScript(script)
    }

    @Test func theOutlineFollowsThePointerToWhatsDirectlyUnderIt() async throws {
        let (engine, server) = try await loaded()
        defer { engine.close(); server.stop() }
        func hover(_ x: Double, _ y: Double, deep: Bool = false) async throws -> String? {
            try await state(engine, "hover", ["x": x, "y": y, "deep": deep]).hovered?.label
        }
        // Over a child, the child.
        #expect(try await hover(300, 160) == "a#buy.cta")
        // In a card's space around its children, the card.
        #expect(try await hover(500, 280) == "div.plan.pro")
        // Between the cards, their row.
        #expect(try await hover(535, 160) == "div.plans")
        // A fragment of a line of text, even inside a link in it, is the line.
        #expect(try await hover(274, 206) == "p.price")
        // ⌘ reaches the fragment.
        #expect(try await hover(274, 206, deep: true) == "b")

        // A click selects what's outlined; its group is framed.
        var page = try await state(engine, "select", ["x": 300, "y": 160])
        #expect(page.selected?.label == "a#buy.cta")
        #expect(page.selected?.rect == .init(x: 290, y: 150, width: 120, height: 30))
        #expect(page.scope?.label == "div.plan.pro")
        #expect(page.scope?.hint == "Pricing.tsx:42")
        #expect(page.hovered == nil)
        #expect(page.crumbs == ["div#root", "div.app", "main", "div.plans", "div.plan.pro", "a#buy.cta"])
        // Clicking the selection again keeps it.
        page = try await state(engine, "select", ["x": 300, "y": 160])
        #expect(page.selected?.label == "a#buy.cta")
        // Hovering the selection outlines nothing more.
        #expect(try await hover(300, 160) == nil)
        // A click on the card's space selects the card.
        page = try await state(engine, "select", ["x": 500, "y": 280])
        #expect(page.selected?.label == "div.plan.pro")
        #expect(page.selected?.rect == .init(x: 270, y: 100, width: 260, height: 200))
        #expect(page.scope?.label == "div.plans")
        // A click in another card selects what's under it there.
        page = try await state(engine, "select", ["x": 30, "y": 160])
        #expect(page.selected?.label == "a.cta")
        #expect(page.selected?.hint == "free-cta")
    }

    @Test func aDeepClickPicksTheInnermostElementAndStepsGoOutAndBackIn() async throws {
        let (engine, server) = try await loaded()
        defer { engine.close(); server.stop() }
        var page = try await state(engine, "select", ["x": 300, "y": 160, "deep": true])
        #expect(page.selected?.label == "a#buy.cta")
        #expect(page.canParent && !page.canChild)
        page = try await state(engine, "step", ["to": "parent"])
        #expect(page.selected?.label == "div.plan.pro")
        page = try await state(engine, "step", ["to": "parent"])
        #expect(page.selected?.label == "div.plans")
        #expect(page.canChild)
        page = try await state(engine, "step", ["to": "child"])
        #expect(page.selected?.label == "div.plan.pro")
        page = try await state(engine, "step", ["to": "child"])
        #expect(page.selected?.label == "a#buy.cta")
        page = try await state(engine, "step", ["to": "crumb", "index": 2])
        #expect(page.selected?.label == "main")
        page = try await state(engine, "clear")
        #expect(page == BrowserAnnotationPageState(viewport: .init(width: 800, height: 600)))
    }

    @Test func aCaptureNamesTheElementWithBoundedFields() async throws {
        let (engine, server) = try await loaded()
        defer { engine.close(); server.stop() }
        _ = try await run(engine, "select", ["x": 300, "y": 160, "deep": true])
        let (capture, _) = try await self.capture(engine)
        #expect(capture.page.title == "Pricing — Acme")
        #expect(capture.page.viewport == .init(width: 800, height: 600))
        #expect(capture.frame == .init(kind: "top", selector: nil, src: nil))
        #expect(capture.element.tag == "a")
        #expect(capture.element.id == "buy")
        #expect(capture.element.classes == ["cta"])
        #expect(capture.element.text == "Upgrade to Pro")
        #expect(capture.element.selector == "#buy")
        #expect(capture.element.path == "body > div#root > div.app > main > div.plans > div.plan.pro > a#buy.cta")
        #expect(capture.element.rect == .init(x: 290, y: 150, width: 120, height: 30))
        #expect(capture.element.attributes == [["href", "/checkout?plan=pro"], ["aria-label", "Upgrade to the Pro plan"]])
        #expect(capture.element.styles.first == ["display", "block"])
        #expect(capture.element.html == #"<a class="cta" id="buy" href="/checkout?plan=pro" aria-label="Upgrade to the Pro plan">Upgrade to Pro</a>"#)
        #expect(capture.source == .init(attributes: [["data-inspector-relative-path", "src/pages/Pricing.tsx"],
                                                     ["data-inspector-line", "42"]], on: "div.plan.pro", distance: 1))
        // The card's HTML leaves its source attributes to the Source line.
        _ = try await run(engine, "step", ["to": "parent"])
        let (card, _) = try await self.capture(engine)
        #expect(card.source?.distance == 0)
        #expect(card.element.html.hasPrefix(#"<div class="plan pro"><h3>Pro</h3>"#))

        _ = try await run(engine, "select", ["x": 500, "y": 540, "deep": true])
        let (long, _) = try await self.capture(engine)
        #expect(long.element.label == "div.long")
        #expect(long.element.selector == "div.long")
        #expect(long.element.text.count == 200)
        #expect(long.element.text.hasSuffix("…"))
        #expect(long.element.attributes.count == 12)
        #expect(long.element.html.hasSuffix("Lorem ipsum dolor sit a…</div>"))
        // A large element's HTML stops at the limit.
        _ = try await run(engine, "select", ["x": 300, "y": 160])
        _ = try await run(engine, "step", ["to": "crumb", "index": 2])
        let (main, _) = try await self.capture(engine)
        #expect(main.element.label == "main")
        #expect(main.element.html.count <= 1201)
        #expect(main.element.html.hasSuffix("…"))
        #expect(main.element.htmlTruncated)
    }

    @Test func selectorsAreUniqueOnThePage() async throws {
        let (engine, server) = try await loaded()
        defer { engine.close(); server.stop() }
        let points: [(Double, Double)] = [(30, 160), (300, 160), (560, 160), (560, 110), (290, 120), (100, 330), (600, 590)]
        var selectors: [String] = []
        for (x, y) in points {
            _ = try await run(engine, "select", ["x": x, "y": y, "deep": true])
            let (capture, _) = try await self.capture(engine)
            let check = """
                (() => { const found = document.querySelectorAll(\(String(reflecting: capture.element.selector)));
                  const box = found[0] && found[0].getBoundingClientRect();
                  return found.length === 1 && box.left <= \(x) && box.right >= \(x) && box.top <= \(y) && box.bottom >= \(y); })()
                """
            #expect(try await page(engine, check) as? Bool == true, "\(capture.element.selector) at \(x),\(y)")
            selectors.append(capture.element.selector)
        }
        #expect(selectors.first == #"[data-testid="free-cta"]"#)
        #expect(selectors[2] == "div.plan:nth-of-type(3) > a.cta")
    }

    @Test func aCaptureHoldsNoFieldValuesCookiesStorageOrSecretAttributes() async throws {
        let (engine, server) = try await loaded()
        defer { engine.close(); server.stop() }
        #expect(try await page(engine, "document.cookie") as? String == "sid=cookie-SECRET")
        // The form, from inside it.
        _ = try await run(engine, "select", ["x": 5, "y": 325, "deep": true])
        var page = try await state(engine, "step", ["to": "parent"])
        while page.selected?.label != "form.login", page.canParent { page = try await state(engine, "step", ["to": "parent"]) }
        #expect(page.selected?.label == "form.login")
        let (form, json) = try await self.capture(engine)
        for secret in ["hunter2", "tok-SECRET", "draft-SECRET", "editable-SECRET", "grace@example.com", "typed-SECRET",
                       "cookie-SECRET", "storage-SECRET", "nonce-SECRET", "form-SECRET"] {
            #expect(!json.contains(secret), "\(secret) leaked")
        }
        #expect(form.element.attributes == [["action", "/session?csrf=…"]])
        // A select's options are its value, so only the button's text shows.
        #expect(form.element.text == "Sign in")
        // A link's token is removed from its address.
        _ = try await run(engine, "select", ["x": 30, "y": 160, "deep": true])
        let (link, _) = try await self.capture(engine)
        #expect(link.element.attributes.first == ["href", "/checkout?plan=free&token=…"])
    }

    @Test func shadowRootsAndFramesAreReachedOrNamedHonestly() async throws {
        let (engine, server) = try await loaded()
        defer { engine.close(); server.stop() }
        var page = try await state(engine, "select", ["x": 430, "y": 330, "deep": true])
        #expect(page.selected?.label == "button.inner")
        var (capture, _) = try await self.capture(engine)
        #expect(capture.shadowHosts == ["x-card"])
        #expect(capture.element.path.hasSuffix("main > x-card >>shadow>> button.inner"))
        #expect(capture.element.selector == "button.inner")

        page = try await state(engine, "select", ["x": 430, "y": 430, "deep": true])
        #expect(page.selected?.label == "p#inside")
        #expect(page.selected?.rect == .init(x: 420, y: 420, width: 160, height: 40))
        (capture, _) = try await self.capture(engine)
        #expect(capture.frame.kind == "same-origin")
        #expect(capture.frame.selector == "iframe.framed")
        #expect(capture.element.selector == "#inside")

        page = try await state(engine, "select", ["x": 610, "y": 430, "deep": true])
        #expect(page.selected?.label == "iframe.foreign")
        (capture, _) = try await self.capture(engine)
        #expect(capture.frame.kind == "cross-origin")
        #expect(capture.frame.src?.hasSuffix("/frame") == true)
    }

    @Test func thePageCantSeeTheScript() async throws {
        let (engine, server) = try await loaded()
        defer { engine.close(); server.stop() }
        _ = try await run(engine, "select", ["x": 300, "y": 160])
        #expect(try await page(engine, "typeof globalThis.__kitAnnotate") as? String == "undefined")
        // Nothing is drawn in the page: Kit outlines over it.
        #expect(try await page(engine, "document.querySelectorAll('[data-kit], kit-annotate').length") as? Int == 0)
    }
}
