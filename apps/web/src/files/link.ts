// The file and lines a Markdown link names, as every Kit client reads them
// (testdata/file-links): GitHub's `path#L42`, `#L42-L50`, or `#L42-50`.

/** A file a link names, and the 1-based lines it points at, if any. */
export interface LinkedFile {
  path: string;
  line?: number;
  end?: number;
}

const MAX_LINE = 999_999_999;

/** Lines `start` through `end`, or undefined unless `1 <= start <= end <= MAX_LINE`. */
function lines(start: string, end: string | undefined): { line: number; end?: number } | undefined {
  if (start.length > 9 || (end && end.length > 9)) return undefined;
  const first = Number(start);
  const last = end === undefined ? first : Number(end);
  if (first < 1 || last < first || last > MAX_LINE) return undefined;
  return last > first ? { line: first, end: last } : { line: first };
}

const decode = (value: string) => {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
};

/** The path a `file:` destination names, or undefined unless it is local and absolute. */
function filePath(value: string): string | undefined {
  try {
    const url = new URL(value);
    if (url.protocol !== "file:" || (url.host !== "" && url.host !== "localhost")) return undefined;
    const path = decode(url.pathname);
    return path.startsWith("/") && !path.includes("\0") ? path : undefined;
  } catch {
    return undefined;
  }
}

/** The path a destination without its lines names, or undefined for a web or other address. */
function linkedFile(destination: string): string | undefined {
  let value = destination;
  if (!value || value.startsWith("#")) return undefined;
  if (value.toLowerCase().startsWith("file:")) return filePath(value.replace(/#.*$/, ""));
  // A literal # in a file name is percent-encoded, so one here starts a fragment.
  const hash = value.indexOf("#");
  if (hash >= 0) value = value.slice(0, hash);
  // A scheme, or a colon with none before it, makes an address, not a path.
  if (/^([a-z][a-z0-9+.-]*)?:/i.test(value)) return undefined;
  if (value.startsWith("//")) return undefined;
  return value ? decode(value) : undefined;
}

/**
 * The file and lines a Markdown link destination names, or undefined for a
 * web or other address. Lines follow the path as GitHub writes them:
 * `#L42`, `#L42-L50`, or `#L42-50`. Lines that can't be, such as `#L0` or
 * `#L50-L42`, open the whole file. Any other fragment, such as a heading's
 * `#usage`, is dropped.
 */
export function linkedLocation(destination: string): LinkedFile | undefined {
  let value = destination.trim();
  let found: { line: number; end?: number } | undefined;
  const github = /#L(\d+)(?:-L?(\d+))?$/.exec(value);
  if (github) {
    found = lines(github[1] as string, github[2]);
    value = value.slice(0, github.index);
  }
  const path = linkedFile(value);
  return path === undefined ? undefined : { path, ...found };
}

/** A file's path with its lines as a link names them, as `src/a.go#L42-L50`. */
export function withLines(file: LinkedFile): string {
  if (!file.line) return file.path;
  return `${file.path}#L${file.line}${file.end && file.end > file.line ? `-L${file.end}` : ""}`;
}

/**
 * The file in the session's folder a link opens, with its lines: a relative
 * path starts from `base`, a Markdown file's folder ("" for the session
 * folder's root), or from the session's folder without one. `resolve`
 * places a path in the session's folder; undefined for a file elsewhere.
 */
export function linkTarget(
  file: LinkedFile,
  base: string | undefined,
  resolve: (path: string) => string | undefined,
): LinkedFile | undefined {
  const relative = !file.path.startsWith("/") && file.path !== "~" && !file.path.startsWith("~/");
  const path = resolve(base && relative ? `${base}/${file.path}` : file.path);
  return path ? { ...file, path } : undefined;
}
