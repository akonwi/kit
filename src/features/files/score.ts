/**
 * Fuzzy scoring helpers for file picker matching.
 * Ported from v1 thread-references.ts.
 */

function subsequenceScore(value: string, query: string): number {
  if (!query) return 0;

  let qi = 0;
  let gaps = 0;
  let firstMatch = -1;

  for (let vi = 0; vi < value.length && qi < query.length; vi++) {
    if (value[vi] === query[qi]) {
      if (firstMatch === -1) firstMatch = vi;
      qi++;
    } else if (qi > 0) {
      gaps++;
    }
  }

  if (qi !== query.length) return 0;

  return Math.max(1, 35 - gaps - Math.max(0, firstMatch));
}

/**
 * Score how well `value` matches `query`.
 * Returns 0 for no match, higher = better.
 */
export function scoreMatch(value: string, query: string): number {
  if (!query) return 1;
  const v = value.toLowerCase();
  const q = query.toLowerCase();
  if (v === q) return 100;
  if (v.startsWith(q)) return 85;
  if (v.includes(q)) return 60;
  return subsequenceScore(v, q);
}
