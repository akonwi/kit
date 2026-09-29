import Foundation
import HTTPTypes
import HTTPTypesFoundation
import OpenAPIRuntime

/// Kit-owned transport for generated OpenAPI clients.
///
/// Generated code owns operation serialization while this transport retains the
/// daemon's loopback, bearer-token, no-redirect, and bounded-body policies.
final class OpenAPITransport: ClientTransport, @unchecked Sendable {
    private let endpoint: URL
    private let token: String
    private let instance: String
    private let session: URLSession
    private let maximumRequestBytes = 1 * 1024 * 1024
    private let maximumResponseBytes = 512 * 1024

    init(endpoint: URL, token: String, instance: String, session: URLSession) {
        self.endpoint = endpoint
        self.token = token
        self.instance = instance
        self.session = session
    }

    func send(_ request: HTTPRequest, body: HTTPBody?, baseURL: URL, operationID: String) async throws -> (HTTPResponse, HTTPBody?) {
        guard baseURL == endpoint, let path = request.path,
              let url = URL(string: path, relativeTo: endpoint)?.absoluteURL,
              url.scheme == endpoint.scheme, url.host == endpoint.host, url.port == endpoint.port else {
            throw ClientError.invalidEndpoint
        }
        let complete = HTTPRequest(method: request.method, url: url, headerFields: request.headerFields)
        guard var urlRequest = URLRequest(httpRequest: complete) else { throw ClientError.invalidEndpoint }
        urlRequest.setValue("Bearer " + token, forHTTPHeaderField: "Authorization")
        urlRequest.setValue(instance, forHTTPHeaderField: "X-Kit-Instance-ID")
        urlRequest.setValue(String(kitWireVersion), forHTTPHeaderField: "X-Kit-Protocol-Version")
        if let body {
            urlRequest.httpBody = try await Data(collecting: body, upTo: maximumRequestBytes)
        }

        let (bytes, response) = try await session.bytes(for: urlRequest)
        defer { bytes.task.cancel() }
        guard let response = response as? HTTPURLResponse, let httpResponse = response.httpResponse else {
            throw ClientError.invalidPayload
        }
        if response.expectedContentLength > Int64(maximumResponseBytes) { throw ClientError.oversized }
        var data = Data()
        for try await byte in bytes {
            guard data.count < maximumResponseBytes else { throw ClientError.oversized }
            data.append(byte)
        }
        return (httpResponse, data.isEmpty ? nil : HTTPBody(data))
    }
}
