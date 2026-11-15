import AppKit
import Darwin
import os

/// Marks how far the app has come since its process started, as signposts
/// for Instruments and, when `KIT_LAUNCH_REPORT` names a file, as a JSON
/// report `script/measure_launch.sh` reads.
///
/// The launch ends at the first frame with real content: the selected
/// session's transcript with its rows measured, or the launcher's session
/// list. Each milestone is recorded once, the first time it is reached.
@MainActor
enum LaunchMetrics {
    enum Milestone: String, CaseIterable, Sendable {
        /// `main` was entered; the time before it is dyld and static initializers.
        case main
        /// The SwiftUI app value finished its initializer.
        case appInit
        /// The app delegate, with the window registry and server directory, was created.
        case delegateInit
        /// AppKit is about to finish launching.
        case willFinishLaunching
        /// A session window's content was first evaluated.
        case windowContent
        /// AppKit finished launching; the Dock stops bouncing.
        case didFinishLaunching
        /// A session window was attached to its model.
        case firstWindow
        /// A session window became visible on screen.
        case windowVisible
        /// The local server answered its handshake.
        case serverConnected
        /// A session tab's store received its first snapshot.
        case sessionDelivered
        /// The selected tab showed a session's tiles rather than its outline.
        case sessionShown
        /// The selected tab's transcript, or the launcher's list, has its content.
        case contentReady
        /// The frame holding that content was committed to the window server.
        case firstContentFrame
        /// A composer became first responder.
        case composerFocused
    }

    private static let signposter = OSSignposter(subsystem: "com.akonwi.kit.launch", category: .pointsOfInterest)
    private static var launchInterval: OSSignpostIntervalState?
    private static var reached: [Milestone: Double] = [:]
    private static var finished = false
    private static var windowObserver: NSObjectProtocol?
    private static let reportPath = ProcessInfo.processInfo.environment["KIT_LAUNCH_REPORT"].flatMap { $0.isEmpty ? nil : $0 }
    /// When the process was created, in seconds since 1970.
    static let processStart: Double = {
        var info = kinfo_proc()
        var size = MemoryLayout<kinfo_proc>.stride
        var name: [Int32] = [CTL_KERN, KERN_PROC, KERN_PROC_PID, getpid()]
        guard sysctl(&name, 4, &info, &size, nil, 0) == 0 else { return now() }
        let start = info.kp_proc.p_un.__p_starttime
        return Double(start.tv_sec) + Double(start.tv_usec) / 1_000_000
    }()

    private static func now() -> Double {
        var time = timeval()
        gettimeofday(&time, nil)
        return Double(time.tv_sec) + Double(time.tv_usec) / 1_000_000
    }

    /// Records a milestone the first time it is reached.
    static func mark(_ milestone: Milestone) {
        guard !finished, reached[milestone] == nil else { return }
        reached[milestone] = now()
        if milestone == .main { launchInterval = signposter.beginInterval("Launch") }
        signposter.emitEvent("Launch milestone", "\(milestone.rawValue, privacy: .public)")
        if milestone == .contentReady { awaitCommit() }
        if milestone == .firstContentFrame {
            cpuAtFirstFrame = cpuTimes()
            finish()
        }
    }

    /// The main thread's and the whole process's CPU time at the first
    /// content frame, in ms. Unlike the wall-clock milestones, these don't
    /// include waiting for the server or for other processes, so they show
    /// the app's own launch work steadily on a loaded Mac.
    private static var cpuAtFirstFrame: (main: Double, process: Double)?

    private static func cpuTimes() -> (main: Double, process: Double) {
        var info = thread_basic_info()
        var count = mach_msg_type_number_t(MemoryLayout<thread_basic_info>.size / MemoryLayout<integer_t>.size)
        let thread = mach_thread_self()
        defer { mach_port_deallocate(mach_task_self_, thread) }
        let result = withUnsafeMutablePointer(to: &info) {
            $0.withMemoryRebound(to: integer_t.self, capacity: Int(count)) {
                thread_info(thread, thread_flavor_t(THREAD_BASIC_INFO), $0, &count)
            }
        }
        let main = result == KERN_SUCCESS ? milliseconds(info.user_time) + milliseconds(info.system_time) : 0
        var usage = rusage()
        getrusage(RUSAGE_SELF, &usage)
        let process = milliseconds(usage.ru_utime) + milliseconds(usage.ru_stime)
        return (main, process)
    }

    private static func milliseconds(_ time: time_value_t) -> Double {
        Double(time.seconds) * 1_000 + Double(time.microseconds) / 1_000
    }

    private static func milliseconds(_ time: timeval) -> Double {
        Double(time.tv_sec) * 1_000 + Double(time.tv_usec) / 1_000
    }

    /// Marks `windowVisible` when the first window comes on screen.
    static func observeWindows() {
        windowObserver = NotificationCenter.default.addObserver(forName: NSWindow.didChangeOcclusionStateNotification, object: nil, queue: .main) { note in
            let window = note.object as? NSWindow
            MainActor.assumeIsolated {
                guard let window, window.occlusionState.contains(.visible) else { return }
                mark(.windowVisible)
                if let windowObserver { NotificationCenter.default.removeObserver(windowObserver) }
                windowObserver = nil
            }
        }
    }

    /// Marks the first frame once the current transaction has been committed.
    private static func awaitCommit() {
        // Core Animation commits in a before-waiting observer of order
        // 2,000,000; this one runs after it in the same run loop pass.
        let observer = CFRunLoopObserverCreateWithHandler(nil, CFRunLoopActivity.beforeWaiting.rawValue, false, 2_000_001) { observer, _ in
            CFRunLoopObserverInvalidate(observer)
            MainActor.assumeIsolated { mark(.firstContentFrame) }
        }
        CFRunLoopAddObserver(CFRunLoopGetMain(), observer, .commonModes)
        // Wake the run loop in case nothing else is pending.
        CFRunLoopWakeUp(CFRunLoopGetMain())
    }

    /// Ends the launch interval and, a second later, so a composer's focus
    /// can be included, writes the report.
    private static func finish() {
        if let launchInterval { signposter.endInterval("Launch", launchInterval) }
        launchInterval = nil
        DispatchQueue.main.asyncAfter(deadline: .now() + 1) { MainActor.assumeIsolated { writeReport() } }
    }

    private static func writeReport() {
        finished = true
        guard let reportPath else { return }
        var milestones: [String: Double] = [:]
        for (milestone, time) in reached { milestones[milestone.rawValue] = ((time - processStart) * 1_000_000).rounded() / 1_000 }
        var report: [String: Any] = ["pid": Int(getpid()), "processStart": processStart, "milestonesMs": milestones]
        if let cpu = cpuAtFirstFrame {
            report["cpuMs"] = ["mainThread": (cpu.main * 1_000).rounded() / 1_000, "process": (cpu.process * 1_000).rounded() / 1_000]
        }
        // The report is written off the main thread once the launch is over.
        DispatchQueue.global(qos: .utility).async {
            guard let data = try? JSONSerialization.data(withJSONObject: report, options: [.sortedKeys]) else { return }
            try? data.write(to: URL(fileURLWithPath: reportPath), options: .atomic)
        }
    }
}
