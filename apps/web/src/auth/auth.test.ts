import { describe, expect, test } from "bun:test";
import { clearCredential, defaultDeviceName, loadCredential, saveCredential } from "./credential";
import { shouldReload } from "./reload";

class MemoryStorage implements Storage {
  private values = new Map<string, string>();
  get length() {
    return this.values.size;
  }
  clear() {
    this.values.clear();
  }
  getItem(key: string) {
    return this.values.get(key) ?? null;
  }
  key(index: number) {
    return [...this.values.keys()][index] ?? null;
  }
  removeItem(key: string) {
    this.values.delete(key);
  }
  setItem(key: string, value: string) {
    this.values.set(key, value);
  }
}

describe("credential", () => {
  test("round-trips and clears", () => {
    const storage = new MemoryStorage();
    const credential = {
      token: "t",
      deviceId: "d",
      deviceName: "Chrome on Android",
      pairedAt: "2025-11-07T00:00:00Z",
    };
    saveCredential(credential, storage);
    expect(loadCredential(storage)).toEqual(credential);
    clearCredential(storage);
    expect(loadCredential(storage)).toBeUndefined();
  });

  test("treats a malformed record as none", () => {
    const storage = new MemoryStorage();
    storage.setItem("kit.device", '{"token":""}');
    expect(loadCredential(storage)).toBeUndefined();
    storage.setItem("kit.device", "{");
    expect(loadCredential(storage)).toBeUndefined();
  });

  test("names the browser and its system", () => {
    expect(
      defaultDeviceName(
        "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Mobile Safari/537.36",
      ),
    ).toBe("Chrome on Android");
    expect(
      defaultDeviceName(
        "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15",
      ),
    ).toBe("Safari on Mac");
    expect(defaultDeviceName("Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0")).toBe(
      "Firefox on Linux",
    );
  });
});

describe("shouldReload", () => {
  test("reloads once for each server instance", () => {
    const storage = new MemoryStorage();
    expect(shouldReload("a", storage)).toBe(true);
    expect(shouldReload("a", storage)).toBe(false);
    expect(shouldReload("b", storage)).toBe(true);
  });
});
