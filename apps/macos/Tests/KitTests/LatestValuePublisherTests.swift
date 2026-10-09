import Foundation
import Testing
@testable import Kit

private actor Deliveries {
    private(set) var values: [Int] = []
    func append(_ value: Int) { values.append(value) }
}

struct LatestValuePublisherTests {
    private func wait(_ predicate: () async -> Bool) async throws {
        for _ in 0..<200 {
            if await predicate() { return }
            try await Task.sleep(for: .milliseconds(5))
        }
        Issue.record("Publisher did not reach expected state")
    }

    @Test func liveBurstDeliversLeadingValueThenOnlyTheLatest() async throws {
        let deliveries = Deliveries()
        let publisher = LatestValuePublisher<Int>(interval: .seconds(60)) { await deliveries.append($0) }
        let run = Task { await publisher.run() }
        defer { run.cancel() }
        await publisher.send(1, coalesce: true)
        try await wait { await deliveries.values == [1] }
        for value in 2...4 { await publisher.send(value, coalesce: true) }
        try await Task.sleep(for: .milliseconds(30))
        #expect(await deliveries.values == [1])
        // An authoritative value supersedes held live values without waiting.
        await publisher.send(5, coalesce: false)
        #expect(await deliveries.values == [1, 5])
    }

    @Test func trailingDeliveryPublishesLatestLiveValue() async throws {
        let deliveries = Deliveries()
        let publisher = LatestValuePublisher<Int>(interval: .milliseconds(200)) { await deliveries.append($0) }
        let run = Task { await publisher.run() }
        defer { run.cancel() }
        await publisher.send(1, coalesce: true)
        try await wait { await deliveries.values == [1] }
        for value in 2...20 { await publisher.send(value, coalesce: true) }
        try await wait { await deliveries.values.last == 20 }
        #expect(await deliveries.values == [1, 20])
    }

    @Test func overlappingAuthoritativeSendsDeliverInOrder() async throws {
        let deliveries = Deliveries()
        let publisher = LatestValuePublisher<Int>(interval: .seconds(60)) { value in
            if value == 1 { try? await Task.sleep(for: .milliseconds(50)) }
            await deliveries.append(value)
        }
        let first = Task { await publisher.send(1, coalesce: false) }
        try await Task.sleep(for: .milliseconds(10))
        await publisher.send(2, coalesce: false)
        await first.value
        try await wait { await deliveries.values.count == 2 }
        #expect(await deliveries.values == [1, 2])
    }

    @Test func cancelledRunDoesNotDeliverHeldLiveValues() async throws {
        let deliveries = Deliveries()
        let publisher = LatestValuePublisher<Int>(interval: .seconds(60)) { await deliveries.append($0) }
        let run = Task { await publisher.run() }
        await publisher.send(1, coalesce: true)
        try await wait { await deliveries.values == [1] }
        await publisher.send(2, coalesce: true)
        run.cancel()
        await run.value
        try await Task.sleep(for: .milliseconds(30))
        #expect(await deliveries.values == [1])
    }
}
