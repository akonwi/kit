import Foundation

/// Cumulative server totals, independent of the loaded transcript page.
public struct SessionUsage: Decodable, Sendable, Equatable {
    public let input: Int
    public let output: Int
    public let cacheRead: Int
    public let cacheWrite: Int
    public let reasoning: Int
    public let total: Int
    public let cost: Double

    public init(_ wire: WireSessionUsage) {
        input = wire.input
        output = wire.output
        cacheRead = wire.cacheRead
        cacheWrite = wire.cacheWrite
        reasoning = wire.reasoning
        total = wire.totalTokens
        cost = wire.cost.total
    }

    public static func number(_ value: Int) -> String {
        value.formatted(.number.locale(Locale(identifier: "en_US")))
    }

    public var formattedCost: String {
        String(format: cost > 0 && cost < 0.01 ? "$%.6f" : "$%.2f", locale: Locale(identifier: "en_US"), cost)
    }

    public var rows: [(String, String)] {
        [("Input", Self.number(input)), ("Output", Self.number(output)),
         ("Cache read", Self.number(cacheRead)), ("Cache write", Self.number(cacheWrite)),
         ("Reasoning", Self.number(reasoning)), ("Total", Self.number(total)),
         ("Cost", formattedCost)]
    }
}
