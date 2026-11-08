import { describe, expect, test } from "bun:test";
import { bytes, displayPath, relativeTime, serverUserHome, tokens } from "./format";

describe("relativeTime", () => {
  const now = new Date(2025, 10, 7, 15, 0);
  test("counts minutes and hours today", () => {
    expect(relativeTime(new Date(2025, 10, 7, 14, 59, 30).toISOString(), now)).toBe("now");
    expect(relativeTime(new Date(2025, 10, 7, 14, 48).toISOString(), now)).toBe("12m");
    expect(relativeTime(new Date(2025, 10, 7, 11, 0).toISOString(), now)).toBe("4h");
  });
  test("names yesterday and this week's days", () => {
    expect(relativeTime(new Date(2025, 10, 6, 23, 0).toISOString(), now)).toBe("Yesterday");
    expect(relativeTime(new Date(2025, 10, 3, 9, 0).toISOString(), now)).toBe(
      new Intl.DateTimeFormat(undefined, { weekday: "short" }).format(new Date(2025, 10, 3)),
    );
  });
});

describe("paths", () => {
  test("takes the server user's home as health reports it", () => {
    expect(serverUserHome("/Users/ada")).toBe("/Users/ada");
    expect(serverUserHome("/home/ada/")).toBe("/home/ada");
    expect(serverUserHome("")).toBeUndefined();
  });
  test("shows the home as ~", () => {
    expect(displayPath("/Users/ada/src/kit", "/Users/ada")).toBe("~/src/kit");
    expect(displayPath("/Users/ada", "/Users/ada")).toBe("~");
    expect(displayPath("/Users/adam/x", "/Users/ada")).toBe("/Users/adam/x");
  });
});

test("tokens", () => {
  expect(tokens(82_400)).toBe("82k");
  expect(tokens(1_200_000)).toBe("1.2M");
  expect(tokens(950)).toBe("950");
});

describe("bytes", () => {
  test("counts in decimal units, as the Mac does", () => {
    expect(bytes(1)).toBe("1 byte");
    expect(bytes(412)).toBe("412 bytes");
    expect(bytes(18_400)).toBe("18 KB");
    expect(bytes(412_000)).toBe("412 KB");
    expect(bytes(2_100_000)).toBe("2.1 MB");
    expect(bytes(3_000_000)).toBe("3 MB");
    expect(bytes(10_485_760)).toBe("10 MB");
  });
});
