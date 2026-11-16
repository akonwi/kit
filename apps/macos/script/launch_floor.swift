// The launch-time floor: an empty SwiftUI app that reports the same
// milestones as Kit (Sources/Kit/Support/LaunchMetrics.swift), so
// `measure_launch.sh --floor` can show how much of a launch is the system's.
import AppKit
import Darwin
import SwiftUI

@MainActor private var reached: [String: Double] = [:]
@MainActor private var mainThreadCPU: [String: Double] = [:]

/// The calling thread's CPU time, in ms.
private func threadCPU() -> Double {
    var info = thread_basic_info()
    var count = mach_msg_type_number_t(MemoryLayout<thread_basic_info>.size / MemoryLayout<integer_t>.size)
    let thread = mach_thread_self()
    defer { mach_port_deallocate(mach_task_self_, thread) }
    let result = withUnsafeMutablePointer(to: &info) {
        $0.withMemoryRebound(to: integer_t.self, capacity: Int(count)) {
            thread_info(thread, thread_flavor_t(THREAD_BASIC_INFO), $0, &count)
        }
    }
    guard result == KERN_SUCCESS else { return 0 }
    return Double(info.user_time.seconds + info.system_time.seconds) * 1_000
        + Double(info.user_time.microseconds + info.system_time.microseconds) / 1_000
}

private func now() -> Double {
    var time = timeval()
    gettimeofday(&time, nil)
    return Double(time.tv_sec) + Double(time.tv_usec) / 1_000_000
}

private func processStart() -> Double {
    var info = kinfo_proc()
    var size = MemoryLayout<kinfo_proc>.stride
    var name: [Int32] = [CTL_KERN, KERN_PROC, KERN_PROC_PID, getpid()]
    guard sysctl(&name, 4, &info, &size, nil, 0) == 0 else { return now() }
    let start = info.kp_proc.p_un.__p_starttime
    return Double(start.tv_sec) + Double(start.tv_usec) / 1_000_000
}

@MainActor private func mark(_ milestone: String) {
    guard reached[milestone] == nil else { return }
    reached[milestone] = now()
    mainThreadCPU[milestone] = threadCPU()
    // Interactive, as Kit defines it: launched, a window on screen, and
    // the main thread about to wait for input.
    if milestone == "didFinishLaunching" || milestone == "windowVisible",
       reached["didFinishLaunching"] != nil, reached["windowVisible"] != nil {
        afterCommit {
            mark("interactive")
            if reached["firstContentFrame"] != nil { report() }
        }
    }
}

/// Runs `body` once the run loop has committed what it drew and is about
/// to wait for input.
@MainActor private func afterCommit(_ body: @escaping @MainActor () -> Void) {
    let observer = CFRunLoopObserverCreateWithHandler(nil, CFRunLoopActivity.beforeWaiting.rawValue, false, 2_000_001) { observer, _ in
        CFRunLoopObserverInvalidate(observer)
        MainActor.assumeIsolated { body() }
    }
    CFRunLoopAddObserver(CFRunLoopGetMain(), observer, .commonModes)
}

@MainActor private var windowObserver: NSObjectProtocol?

@MainActor private func report() {
    guard let path = ProcessInfo.processInfo.environment["KIT_LAUNCH_REPORT"] else { return }
    let start = processStart()
    let milestones = reached.mapValues { (($0 - start) * 1_000_000).rounded() / 1_000 }
    let report: [String: Any] = ["pid": Int(getpid()), "processStart": start, "milestonesMs": milestones,
                                 "mainThreadCPUMs": mainThreadCPU]
    if let data = try? JSONSerialization.data(withJSONObject: report) { try? data.write(to: URL(fileURLWithPath: path)) }
}

private final class Delegate: NSObject, NSApplicationDelegate {
    func applicationWillFinishLaunching(_ notification: Notification) { MainActor.assumeIsolated { mark("willFinishLaunching") } }
    func applicationDidFinishLaunching(_ notification: Notification) { MainActor.assumeIsolated { mark("didFinishLaunching") } }
}

private struct Content: View {
    var body: some View {
        Text("Hello").frame(width: 600, height: 400).onAppear {
            mark("contentReady")
            afterCommit {
                mark("firstContentFrame")
                if reached["interactive"] != nil { report() }
            }
        }
    }
}

private struct FloorApp: App {
    @NSApplicationDelegateAdaptor(Delegate.self) private var delegate
    var body: some Scene { WindowGroup { Content() } }
}

@main enum Floor {
    static func main() {
        MainActor.assumeIsolated {
            mark("main")
            windowObserver = NotificationCenter.default.addObserver(forName: NSWindow.didChangeOcclusionStateNotification, object: nil, queue: .main) { note in
                let window = note.object as? NSWindow
                MainActor.assumeIsolated {
                    guard let window, window.occlusionState.contains(.visible) else { return }
                    mark("windowVisible")
                }
            }
        }
        FloorApp.main()
    }
}
