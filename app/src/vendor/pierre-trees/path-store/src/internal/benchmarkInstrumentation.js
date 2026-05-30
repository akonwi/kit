//#region ../path-store/src/internal/benchmarkInstrumentation.ts
const BENCHMARK_INSTRUMENTATION = Symbol("benchmarkInstrumentation");
/** Attaches instrumentation without changing the public option shape. */
function attachBenchmarkInstrumentation(value, instrumentation) {
	if (instrumentation == null) return value;
	Object.defineProperty(value, BENCHMARK_INSTRUMENTATION, {
		configurable: true,
		enumerable: false,
		value: instrumentation,
		writable: false
	});
	return value;
}
function getBenchmarkInstrumentation(value) {
	if (value == null) return null;
	return value[BENCHMARK_INSTRUMENTATION] ?? null;
}
/** Executes phase timing only when a benchmark fixture injects instrumentation. */
function withBenchmarkPhase(instrumentation, name, fn) {
	if (instrumentation == null) return fn();
	return instrumentation.measurePhase(name, fn);
}
function setBenchmarkCounter(instrumentation, name, value) {
	if (!Number.isFinite(value) || instrumentation == null) return;
	instrumentation.setCounter(name, value);
}

//#endregion
export { attachBenchmarkInstrumentation, getBenchmarkInstrumentation, setBenchmarkCounter, withBenchmarkPhase };
//# sourceMappingURL=benchmarkInstrumentation.js.map