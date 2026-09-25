import Foundation

/// Cumulative server totals, independent of the loaded transcript page.
struct SessionUsage: Decodable, Sendable {
    let input: Int
    let output: Int
    let cacheRead: Int
    let cacheWrite: Int
    let reasoning: Int
    let total: Int
    let cost: Double

    init(_ wire: WireSessionUsage) {
        input = wire.input
        output = wire.output
        cacheRead = wire.cacheRead
        cacheWrite = wire.cacheWrite
        reasoning = wire.reasoning
        total = wire.totalTokens
        cost = wire.cost.total
    }

    static func number(_ value: Int) -> String {
        value.formatted(.number.locale(Locale(identifier: "en_US")))
    }

    var formattedCost: String {
        String(format: cost > 0 && cost < 0.01 ? "$%.6f" : "$%.2f", locale: Locale(identifier: "en_US"), cost)
    }

    var rows: [(String, String)] {
        [("Input", Self.number(input)), ("Output", Self.number(output)),
         ("Cache read", Self.number(cacheRead)), ("Cache write", Self.number(cacheWrite)),
         ("Reasoning", Self.number(reasoning)), ("Total", Self.number(total)),
         ("Cost", formattedCost)]
    }
}
