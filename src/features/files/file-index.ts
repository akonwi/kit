/**
 * Lazy file index — scans on first access, caches the result.
 * Provides scored file suggestions for a query string.
 */

import { scanFiles, type ScanResult } from "./scan-files";
import { scoreMatch } from "./score";

export type FileIndexEntry = {
  /** Raw relative path (files without prefix, dirs with trailing /) */
  path: string;
  /** Whether this is a directory */
  isDir: boolean;
};

export type FileSuggestion = {
  name: string;
  description: string;
  value: string;
};

export function createFileIndex(cwd: string) {
  let cached: FileIndexEntry[] | null = null;
  let scanning: Promise<FileIndexEntry[]> | null = null;

  async function doScan(): Promise<FileIndexEntry[]> {
    const result: ScanResult = await scanFiles(cwd);
    const entries: FileIndexEntry[] = [
      ...result.dirs.map((p) => ({ path: p, isDir: true })),
      ...result.files.map((p) => ({ path: p, isDir: false })),
    ];
    return entries;
  }

  async function ensureLoaded(): Promise<FileIndexEntry[]> {
    if (cached) return cached;
    if (!scanning) {
      scanning = doScan().then((entries) => {
        cached = entries;
        scanning = null;
        return entries;
      });
    }
    return scanning;
  }

  /**
   * Get file suggestions matching a query.
   * Triggers a scan on first call (lazy).
   * Returns scored and sorted results.
   */
  async function suggest(query: string): Promise<FileSuggestion[]> {
    const entries = await ensureLoaded();
    const norm = query.replace(/^@/, "");

    return entries
      .map((entry) => ({
        entry,
        score: scoreMatch(entry.path, norm),
      }))
      .filter((x) => x.score > 0)
      .sort((a, b) => b.score - a.score || a.entry.path.localeCompare(b.entry.path))
      .map((x) => ({
        name: `@${x.entry.path}`,
        description: x.entry.isDir ? "directory" : "",
        value: x.entry.path,
      }));
  }

  /** Force a rescan (e.g. after file changes) */
  function invalidate() {
    cached = null;
    scanning = null;
  }

  /** Check if the index has been loaded */
  function isLoaded(): boolean {
    return cached !== null;
  }

  return { suggest, invalidate, isLoaded, ensureLoaded };
}

export type FileIndex = ReturnType<typeof createFileIndex>;
