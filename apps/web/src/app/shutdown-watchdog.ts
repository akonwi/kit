type ProcessWithActiveHandles = NodeJS.Process & {
	_getActiveHandles?: () => unknown[];
	_getActiveRequests?: () => unknown[];
};

let shutdownWatchdogStarted = false;

function describeActiveHandle(handle: unknown): string {
	if (typeof handle !== "object" || handle === null) return typeof handle;
	const constructorName = handle.constructor?.name;
	return constructorName || Object.prototype.toString.call(handle);
}

function reportDanglingHandlesForDebugging(): void {
	if (!process.env.KIT_DEBUG_SHUTDOWN) return;
	const proc = process as ProcessWithActiveHandles;
	const handles = proc._getActiveHandles?.() ?? [];
	const requests = proc._getActiveRequests?.() ?? [];
	console.error(
		`[kit] forcing shutdown with ${handles.length} active handle(s), ${requests.length} active request(s)`,
	);
	for (const handle of handles) {
		console.error(`[kit] active handle: ${describeActiveHandle(handle)}`);
	}
	for (const request of requests) {
		console.error(`[kit] active request: ${describeActiveHandle(request)}`);
	}
}

/**
 * Last-resort process shutdown after the lifecycle owner has completed all
 * orderly cleanup. The unref'd timer never delays a naturally draining process.
 */
export function startShutdownWatchdog(exitCode: number): void {
	if (shutdownWatchdogStarted) return;
	shutdownWatchdogStarted = true;
	const shutdownWatchdog = setTimeout(() => {
		reportDanglingHandlesForDebugging();
		process.exit(exitCode);
	}, 200);
	shutdownWatchdog.unref?.();
}
