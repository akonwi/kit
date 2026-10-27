// Kit's browser agent page script (ADR 0051). It runs in an isolated
// content world: the page shares the DOM with it but cannot see its globals,
// so a page cannot read, forge, or remove refs. Each operation returns a
// JSON string.
globalThis.__kitAgent ||= (() => {
  const OUTLINE_LIMIT = 40000;
  const NAME_LIMIT = 120;
  const TEXT_LIMIT = 300;
  const VALUE_LIMIT = 300;

  // Refs stay the same for an element for the life of its document.
  let nextRef = 1;
  const refByElement = new WeakMap();
  const elementByRef = new Map();

  function refFor(element) {
    let ref = refByElement.get(element);
    if (!ref) {
      ref = "e" + nextRef++;
      refByElement.set(element, ref);
      elementByRef.set(ref, new WeakRef(element));
    }
    return ref;
  }

  function lookup(ref) {
    const element = elementByRef.get(ref)?.deref();
    if (!element || !element.isConnected) {
      elementByRef.delete(ref);
      return null;
    }
    return element;
  }

  const collapse = (text) => (text || "").replace(/\s+/g, " ").trim();
  const clip = (text, limit) => (text.length > limit ? text.slice(0, limit - 1) + "…" : text);
  const quote = (text) => JSON.stringify(text);

  const TEXT_INPUTS = new Set(["", "text", "search", "email", "tel", "url", "password", "number",
    "date", "datetime-local", "month", "week", "time"]);
  const SECRET_AUTOCOMPLETE = /\b(current-password|new-password|one-time-code|cc-number|cc-csc|cc-exp(-month|-year)?)\b/;

  function inputType(element) {
    return (element.getAttribute("type") || "").toLowerCase();
  }

  function isTextEntry(element) {
    if (element instanceof HTMLTextAreaElement) return true;
    if (element instanceof HTMLInputElement) return TEXT_INPUTS.has(inputType(element));
    return element.isContentEditable === true;
  }

  function isSecret(element) {
    if (!(element instanceof HTMLInputElement)) return false;
    if (inputType(element) === "password") return true;
    return SECRET_AUTOCOMPLETE.test((element.getAttribute("autocomplete") || "").toLowerCase());
  }

  function role(element) {
    const explicit = (element.getAttribute("role") || "").trim().split(/\s+/)[0];
    if (explicit && explicit !== "none" && explicit !== "presentation") return explicit;
    const tag = element.localName;
    switch (tag) {
      case "a": case "area": return element.hasAttribute("href") ? "link" : null;
      case "button": case "summary": return "button";
      case "input": {
        const type = inputType(element);
        if (type === "hidden") return null;
        if (["button", "submit", "reset", "image"].includes(type)) return "button";
        if (type === "checkbox") return "checkbox";
        if (type === "radio") return "radio";
        if (type === "range") return "slider";
        if (type === "number") return "spinbutton";
        if (type === "search") return "searchbox";
        if (type === "file") return "file";
        if (type === "color") return "color";
        return "textbox";
      }
      case "textarea": return "textbox";
      case "select": return element.multiple || element.size > 1 ? "listbox" : "combobox";
      case "option": return "option";
      case "h1": case "h2": case "h3": case "h4": case "h5": case "h6": return "heading";
      case "img": return element.getAttribute("alt") === "" ? null : "img";
      case "nav": return "navigation";
      case "main": return "main";
      case "header": return "banner";
      case "footer": return "contentinfo";
      case "aside": return "complementary";
      case "form": return "form";
      case "dialog": return "dialog";
      case "ul": case "ol": return "list";
      case "li": return "listitem";
      case "table": return "table";
      case "tr": return "row";
      case "th": return "columnheader";
      case "td": return "cell";
      case "iframe": return "iframe";
      case "label": return "label";
      case "p": return "paragraph";
      case "progress": return "progressbar";
    }
    if (element.isContentEditable && element.parentElement?.isContentEditable !== true) return "textbox";
    return null;
  }

  const INTERACTIVE = new Set(["link", "button", "textbox", "searchbox", "checkbox", "radio", "combobox",
    "listbox", "option", "slider", "spinbutton", "switch", "tab", "menuitem", "menuitemcheckbox",
    "menuitemradio", "treeitem", "file", "color"]);
  // Roles the interactive outline keeps for context.
  const CONTEXT = new Set(["heading", "dialog", "alert", "alertdialog", "iframe"]);
  // Roles that name themselves from their content.
  const NAMED_BY_CONTENT = new Set(["link", "button", "heading", "tab", "menuitem", "menuitemcheckbox",
    "menuitemradio", "option", "treeitem", "cell", "columnheader", "label", "listitem", "switch",
    "checkbox", "radio"]);

  function textOf(element) {
    return collapse(element.innerText ?? element.textContent ?? "");
  }

  function name(element, roleName) {
    const labelledBy = element.getAttribute("aria-labelledby");
    if (labelledBy) {
      const text = labelledBy.split(/\s+/).map((id) => element.ownerDocument.getElementById(id))
        .filter(Boolean).map(textOf).join(" ");
      if (collapse(text)) return collapse(text);
    }
    const aria = collapse(element.getAttribute("aria-label"));
    if (aria) return aria;
    if (element.labels && element.labels.length) {
      const text = collapse([...element.labels].map(textOf).join(" "));
      if (text) return text;
    }
    if (element instanceof HTMLInputElement && ["button", "submit", "reset"].includes(inputType(element))) {
      return collapse(element.value) || inputType(element);
    }
    if (element instanceof HTMLImageElement || element.localName === "area") {
      const alt = collapse(element.getAttribute("alt"));
      if (alt) return alt;
    }
    if (roleName && NAMED_BY_CONTENT.has(roleName)) {
      const text = textOf(element);
      if (text) return text;
    }
    const placeholder = collapse(element.getAttribute("placeholder"));
    if (placeholder) return placeholder;
    return collapse(element.getAttribute("title"));
  }

  function hidden(element) {
    if (element.hidden || element.inert || element.getAttribute("aria-hidden") === "true") return true;
    if (typeof element.checkVisibility === "function") {
      // An element without a box of its own, like display: contents, may
      // still hold visible children.
      return !element.checkVisibility({ visibilityProperty: true }) && getComputedStyle(element).display !== "contents";
    }
    const style = getComputedStyle(element);
    return style.display === "none" || style.visibility === "hidden";
  }

  const SKIPPED = new Set(["script", "style", "noscript", "template", "head", "meta", "link"]);

  function states(element, roleName) {
    const parts = [];
    if (element.localName === "h1" || /^h[2-6]$/.test(element.localName)) parts.push("level=" + element.localName[1]);
    else if (roleName === "heading" && element.getAttribute("aria-level")) parts.push("level=" + element.getAttribute("aria-level"));
    if (isSecret(element)) parts.push("secret");
    if ("checked" in element && ["checkbox", "radio"].includes(roleName) && element instanceof HTMLInputElement) {
      parts.push(element.checked ? "checked" : "unchecked");
    } else if (element.getAttribute("aria-checked")) {
      parts.push(element.getAttribute("aria-checked") === "true" ? "checked" : "unchecked");
    }
    if (element.getAttribute("aria-expanded")) parts.push(element.getAttribute("aria-expanded") === "true" ? "expanded" : "collapsed");
    if (element.getAttribute("aria-selected") === "true" || (element instanceof HTMLOptionElement && element.selected)) parts.push("selected");
    if (element.disabled === true || element.getAttribute("aria-disabled") === "true") parts.push("disabled");
    if (element.readOnly === true) parts.push("readonly");
    if (element.required === true) parts.push("required");
    if (element === element.ownerDocument.activeElement && element !== element.ownerDocument.body) parts.push("focused");
    return parts;
  }

  function value(element, roleName) {
    if (element instanceof HTMLSelectElement) {
      const chosen = [...element.selectedOptions].map((option) => collapse(option.label || option.text));
      return chosen.length ? chosen.join(", ") : null;
    }
    if (element instanceof HTMLInputElement) {
      const type = inputType(element);
      if (["checkbox", "radio", "button", "submit", "reset", "image", "file"].includes(type)) return null;
      return element.value;
    }
    if (element instanceof HTMLTextAreaElement) return element.value;
    if (roleName === "textbox" && element.isContentEditable) return element.innerText;
    return null;
  }

  function line(element, roleName) {
    let text = roleName;
    const label = clip(name(element, roleName), NAME_LIMIT);
    if (label) text += " " + quote(label);
    if (INTERACTIVE.has(roleName) || roleName === "iframe") text += " [ref=" + refFor(element) + "]";
    const flags = states(element, roleName);
    if (flags.length) text += " " + flags.join(" ");
    const current = value(element, roleName);
    if (current !== null && current !== "") text += " value=" + quote(clip(current, VALUE_LIMIT));
    if (roleName === "link") {
      const href = element.getAttribute("href");
      if (href && !href.startsWith("javascript:")) text += " -> " + clip(href, 200);
    }
    if (element instanceof HTMLSelectElement) {
      const options = [...element.options].slice(0, 25).map((option) => quote(clip(collapse(option.label || option.text), 60)));
      if (options.length) text += " options=[" + options.join(", ") + (element.options.length > 25 ? ", …" : "") + "]";
    }
    return text;
  }

  // Builds the outline. In interactive mode only controls and headings are
  // listed; full mode adds landmarks and the page's text.
  function outline(full) {
    const lines = [];
    let length = 0;
    let truncated = false;
    const push = (depth, text) => {
      if (truncated) return;
      const entry = "  ".repeat(Math.min(depth, 12)) + "- " + text;
      if (length + entry.length + 1 > OUTLINE_LIMIT) { truncated = true; return; }
      lines.push(entry);
      length += entry.length + 1;
    };

    // `quiet` is set inside a label whose control is listed with its text
    // as its name, so the text isn't repeated.
    const walk = (node, depth, quiet = false) => {
      if (truncated) return;
      for (let child = node.firstChild; child; child = child.nextSibling) {
        if (truncated) return;
        if (child.nodeType === Node.TEXT_NODE) {
          if (!full || quiet) continue;
          const text = collapse(child.data);
          if (text) push(depth, "text: " + quote(clip(text, TEXT_LIMIT)));
          continue;
        }
        if (child.nodeType !== Node.ELEMENT_NODE) continue;
        const element = child;
        if (SKIPPED.has(element.localName)) continue;
        if (hidden(element)) continue;
        const roleName = role(element);
        const listed = roleName && (INTERACTIVE.has(roleName) || CONTEXT.has(roleName)
          || (full && !["label", "paragraph", "listitem", "cell", "row"].includes(roleName)));
        let next = depth;
        if (listed) {
          push(depth, line(element, roleName));
          next = depth + 1;
          // A control's own text is its name; a custom list or menu keeps
          // its options.
          const container = ["listbox", "combobox", "menu", "menubar", "tablist", "tree", "grid"].includes(roleName)
            && !(element instanceof HTMLSelectElement);
          if ((INTERACTIVE.has(roleName) || roleName === "heading") && !container) continue;
        }
        if (element.localName === "iframe") {
          let inner = null;
          try { inner = element.contentDocument; } catch { inner = null; }
          if (inner?.body) walk(inner.body, next, quiet);
          else if (full) push(next, "text: \"(cross-origin frame, not shown)\"");
          continue;
        }
        const labels = quiet || (element.localName === "label" && element.control != null && !hidden(element.control));
        if (element.shadowRoot) walk(element.shadowRoot, next, labels);
        walk(element, next, labels);
      }
    };
    if (document.body) walk(document.body, 0);
    if (truncated) lines.push("- (outline truncated at " + OUTLINE_LIMIT + " characters)");
    return lines.join("\n");
  }

  function snapshot(args) {
    return { title: document.title || "", url: location.href, outline: outline(args.mode !== "interactive") };
  }

  // Scrolls the element into view and reports where its centre is, for the
  // agent pointer, in the viewport's CSS pixels.
  function locate(args) {
    const element = lookup(args.ref);
    if (!element) return { error: "Unknown ref " + args.ref + ". Take a new snapshot; the page may have changed." };
    if (args.scroll !== false) element.scrollIntoView({ block: "center", inline: "center", behavior: "instant" });
    const box = element.getBoundingClientRect();
    return {
      x: box.left + box.width / 2, y: box.top + box.height / 2,
      viewportWidth: window.innerWidth, viewportHeight: window.innerHeight,
    };
  }

  function mouse(element, type, box) {
    const init = { bubbles: true, cancelable: true, composed: true, view: window, button: 0,
      clientX: box.left + box.width / 2, clientY: box.top + box.height / 2 };
    const Pointer = typeof PointerEvent === "function" ? PointerEvent : MouseEvent;
    if (type.startsWith("pointer")) element.dispatchEvent(new Pointer(type, { ...init, pointerId: 1, pointerType: "mouse", isPrimary: true }));
    else element.dispatchEvent(new MouseEvent(type, init));
  }

  function click(element) {
    const box = element.getBoundingClientRect();
    mouse(element, "pointerover", box);
    mouse(element, "mouseover", box);
    mouse(element, "pointerdown", box);
    mouse(element, "mousedown", box);
    if (typeof element.focus === "function") element.focus({ preventScroll: true });
    mouse(element, "pointerup", box);
    mouse(element, "mouseup", box);
    element.click();
  }

  // Sets a field's value as typing would, through the element's own setter
  // so frameworks that track the value see the change.
  function setValue(element, text) {
    if (typeof element.focus === "function") element.focus({ preventScroll: true });
    if (element.isContentEditable && !(element instanceof HTMLInputElement || element instanceof HTMLTextAreaElement)) {
      element.textContent = text;
    } else {
      const prototype = element instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
      Object.getOwnPropertyDescriptor(prototype, "value").set.call(element, text);
    }
    element.dispatchEvent(new InputEvent("input", { bubbles: true, composed: true, inputType: "insertReplacementText", data: text }));
    element.dispatchEvent(new Event("change", { bubbles: true }));
  }

  function press(element, key) {
    const target = element || document.activeElement || document.body;
    const init = { key, code: key.length === 1 ? "Key" + key.toUpperCase() : key, bubbles: true, cancelable: true, composed: true };
    const proceed = target.dispatchEvent(new KeyboardEvent("keydown", init));
    if (proceed && key.length === 1) target.dispatchEvent(new KeyboardEvent("keypress", init));
    if (proceed && key === "Enter") {
      if (target instanceof HTMLInputElement && target.form) {
        target.form.requestSubmit();
      } else if (target instanceof HTMLAnchorElement || target instanceof HTMLButtonElement) {
        target.click();
      }
    } else if (proceed && key.length === 1 && isTextEntry(target) && !(target.readOnly || target.disabled)) {
      if (target.isContentEditable && !(target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement)) {
        document.execCommand("insertText", false, key);
      } else {
        setValue(target, (target.value || "") + key);
      }
    }
    target.dispatchEvent(new KeyboardEvent("keyup", init));
  }

  // Validates the action now and performs it on the next task, so a page
  // dialog it opens never blocks this script.
  function act(args) {
    const later = (work) => setTimeout(() => { try { work(); } catch {} }, 0);
    if (args.action === "scroll" && !args.ref) {
      const direction = args.direction === "up" ? -1 : 1;
      window.scrollBy({ top: direction * window.innerHeight * 0.8, behavior: "instant" });
      return { ok: true };
    }
    if (args.action === "press" && !args.ref) {
      later(() => press(null, args.key));
      return { ok: true };
    }
    const element = lookup(args.ref);
    if (!element) return { error: "Unknown ref " + args.ref + ". Take a new snapshot; the page may have changed." };
    const disabled = element.disabled === true || element.getAttribute("aria-disabled") === "true";
    switch (args.action) {
      case "click":
        if (disabled) return { error: "The element is disabled." };
        later(() => click(element));
        return { ok: true };
      case "focus":
        if (typeof element.focus !== "function") return { error: "The element can't take focus." };
        element.focus({ preventScroll: true });
        return { ok: true };
      case "scroll":
        element.scrollIntoView({ block: "center", inline: "center", behavior: "instant" });
        return { ok: true };
      case "fill":
        if (!isTextEntry(element)) return { error: "fill needs a text field; use select, check, or click for other controls." };
        if (disabled || element.readOnly) return { error: "The field is disabled or read-only." };
        later(() => setValue(element, args.text));
        return { ok: true };
      case "select": {
        if (!(element instanceof HTMLSelectElement)) return { error: "select needs a select element; click custom menus instead." };
        if (disabled) return { error: "The element is disabled." };
        const wanted = args.text;
        const option = [...element.options].find((item) => item.value === wanted)
          || [...element.options].find((item) => collapse(item.label || item.text) === collapse(wanted));
        if (!option) return { error: "No option has the value or label " + quote(wanted) + "." };
        later(() => {
          element.focus({ preventScroll: true });
          option.selected = true;
          element.dispatchEvent(new Event("input", { bubbles: true, composed: true }));
          element.dispatchEvent(new Event("change", { bubbles: true }));
        });
        return { ok: true };
      }
      case "check":
      case "uncheck": {
        const wanted = args.action === "check";
        const native = element instanceof HTMLInputElement && ["checkbox", "radio"].includes(inputType(element));
        const aria = ["checkbox", "radio", "switch", "menuitemcheckbox", "menuitemradio"].includes(element.getAttribute("role") || "");
        if (!native && !aria) return { error: args.action + " needs a checkbox, radio button, or switch." };
        if (disabled) return { error: "The element is disabled." };
        const current = native ? element.checked : element.getAttribute("aria-checked") === "true";
        if (current === wanted) return { ok: true, unchanged: true };
        if (!wanted && native && inputType(element) === "radio") return { error: "A radio button is unchecked by checking another." };
        later(() => click(element));
        return { ok: true };
      }
      case "press":
        later(() => press(element, args.key));
        return { ok: true };
    }
    return { error: "Unsupported action." };
  }

  return {
    run(op, args) {
      switch (op) {
        case "snapshot": return JSON.stringify(snapshot(args));
        case "locate": return JSON.stringify(locate(args));
        case "act": return JSON.stringify(act(args));
      }
      return JSON.stringify({ error: "Unknown operation." });
    },
  };
})();
