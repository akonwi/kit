import { expect, describe as suite, test } from "bun:test";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { splitTextComments } from "./textComments";

// Each case is `<case>.txt`, a whole message and one newline after it, and
// `<case>.json`, what a client reads from it (testdata/text-annotations/README.md).
const fixtures = join(import.meta.dir, "../../../../testdata/text-annotations");
const cases = readdirSync(fixtures)
  .filter((name) => name.endsWith(".txt"))
  .map((name) => name.slice(0, -".txt".length));

suite("comments on selected text", () => {
  test("the fixtures are present", () => {
    expect(cases.length).toBeGreaterThanOrEqual(9);
  });

  for (const name of cases)
    test(`reads like the fixture: ${name}`, () => {
      const message = readFileSync(join(fixtures, `${name}.txt`), "utf8").slice(0, -1);
      const expected = JSON.parse(readFileSync(join(fixtures, `${name}.json`), "utf8"));
      expect(splitTextComments(message)).toEqual({ text: expected.text, comments: expected.comments });
    });
});
