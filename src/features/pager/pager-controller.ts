/**
 * Pager controller — manages section navigation, per-section notes,
 * and feedback submission for long assistant responses.
 */

import { createSignal, createMemo } from "solid-js";
import type { AgentMessage } from "@mariozechner/pi-agent-core";
import type { AgentRuntime } from "../../backend";
import { splitSections, type PagerSection } from "./split-sections";

function extractAssistantText(msg: AgentMessage): string {
  const content: unknown = (msg as { content?: unknown }).content;
  if (typeof content === "string") return content;
  if (!Array.isArray(content)) return "";
  return content
    .map((block) => {
      if (!block || typeof block !== "object") return "";
      const b = block as Record<string, unknown>;
      if (b.type === "text" && typeof b.text === "string") return b.text;
      return "";
    })
    .filter(Boolean)
    .join("\n")
    .trim();
}

function formatFeedbackMessage(sections: PagerSection[], notes: Map<number, string>): string | null {
  const blocks: string[] = [];

  sections.forEach((section, idx) => {
    const note = notes.get(idx)?.trim();
    if (!note) return;
    blocks.push(`## ${section.title}\n${note}`);
  });

  if (blocks.length === 0) return null;

  return [
    "Here is my feedback on your previous response, grouped by section.",
    "",
    ...blocks.flatMap((block, idx) => (idx === 0 ? [block] : ["", block])),
    "",
    "Please use this section-specific feedback in your revision or reply.",
  ].join("\n");
}

export function createPagerController(runtime: AgentRuntime) {
  const [sections, setSections] = createSignal<PagerSection[]>([]);
  const [currentIndex, setCurrentIndex] = createSignal(0);
  const [notes, setNotes] = createSignal<Map<number, string>>(new Map());
  const [active, setActive] = createSignal(false);

  const currentSection = createMemo(() => {
    const s = sections();
    const idx = currentIndex();
    return idx >= 0 && idx < s.length ? s[idx] : null;
  });

  /**
   * Try to activate the pager for the last assistant message.
   * Returns true if the pager was activated.
   */
  function tryActivate(messages: AgentMessage[]): boolean {
    // Find last assistant message
    for (let i = messages.length - 1; i >= 0; i--) {
      const msg = messages[i];
      if (msg.role !== "assistant") continue;

      const text = extractAssistantText(msg);
      if (!text) break;

      const result = splitSections(text);
      if (result.length < 2) break;

      setSections(result);
      setCurrentIndex(0);
      setNotes(new Map());
      setActive(true);
      return true;
    }
    return false;
  }

  // Scroll delegate — PagerView binds its scrollbox ref here
  let scrollDelegate: { scrollBy: (delta: number) => void } | null = null;

  function setScrollDelegate(delegate: { scrollBy: (delta: number) => void } | null) {
    scrollDelegate = delegate;
  }

  function scrollUp() {
    scrollDelegate?.scrollBy(-3);
  }

  function scrollDown() {
    scrollDelegate?.scrollBy(3);
  }

  function close() {
    setActive(false);
    setSections([]);
    setNotes(new Map());
    setCurrentIndex(0);
    scrollDelegate = null;
  }

  function nextSection() {
    const max = sections().length - 1;
    if (currentIndex() < max) {
      setCurrentIndex(currentIndex() + 1);
    }
  }

  function prevSection() {
    if (currentIndex() > 0) {
      setCurrentIndex(currentIndex() - 1);
    }
  }

  function setNote(index: number, text: string) {
    setNotes((prev) => {
      const next = new Map(prev);
      const trimmed = text.trim();
      if (trimmed) {
        next.set(index, trimmed);
      } else {
        next.delete(index);
      }
      return next;
    });
  }

  function getNoteCount(): number {
    return Array.from(notes().values()).filter((n) => n.trim().length > 0).length;
  }

  /**
   * Submit all notes as a structured feedback message.
   * Returns true if feedback was sent.
   */
  async function submitFeedback(): Promise<boolean> {
    const message = formatFeedbackMessage(sections(), notes());
    if (!message) return false;

    close();
    await runtime.submitUserMessage(message);
    return true;
  }

  return {
    get active() { return active(); },
    get sections() { return sections(); },
    get currentIndex() { return currentIndex(); },
    get currentSection() { return currentSection(); },
    get notes() { return notes(); },
    getNoteCount,
    tryActivate,
    close,
    nextSection,
    prevSection,
    setNote,
    submitFeedback,
    setScrollDelegate,
    scrollUp,
    scrollDown,
  };
}

export type PagerController = ReturnType<typeof createPagerController>;
