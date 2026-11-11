// Kit's browser annotation page script. It runs in an isolated content
// world: the page shares the DOM with it but can't see its globals, so the
// page can't read or change what it has selected. It draws nothing in the
// page; Kit draws the outlines over the page's view from the rects it
// reports. It never reads cookies, storage, or the values of form fields.
//
// The outline follows the pointer to what's directly under it: over a
// child, the child; over the space between a container's children, the
// container. Inline fragments of a line of text count as the text's
// element, and a deep pick (with ⌘) takes the innermost element. A click
// selects what's outlined; parent and child step out and back in. Every
// operation returns the same JSON state, except capture.
globalThis.__kitAnnotate ||= (() => {
  const LABEL_LIMIT = 48;
  const TEXT_LIMIT = 200;
  const HTML_LIMIT = 1200;
  const ATTRIBUTE_LIMIT = 12;
  const ATTRIBUTE_VALUE_LIMIT = 120;
  const SELECTOR_LIMIT = 300;
  const PATH_LEVELS = 8;
  const CRUMB_LEVELS = 6;

  let hovered = null;
  let selected = null;
  // Elements stepped out of with parent, for child to step back into.
  let trail = [];

  const collapse = (text) => (text || "").replace(/\s+/g, " ").trim();
  const clip = (text, limit) => (text.length > limit ? text.slice(0, limit - 1) + "…" : text);
  const redact = (url) => (url || "").replace(SECRET_QUERY, "$1…");
  const URL_ATTRIBUTES = new Set(["href", "src", "action", "srcset", "poster", "data"]);

  const SKIPPED = new Set(["script", "style", "noscript", "template", "head", "meta", "link", "title", "base"]);
  // Attribute names that may hold a secret, which are never read.
  const SECRET_NAME = /(^value$|^nonce$|^integrity$|token|secret|password|passwd|csrf|xsrf|session|auth|credential|api[-_]?key)/i;
  // Class names and IDs a build generates, which change between builds.
  const GENERATED = /^(css|sc|jsx|emotion|svelte|astro)-|\d.*\d.*\d|__[A-Za-z0-9_-]{5,}$|^:|:$/;
  // URL query parameters that may carry a secret, whose values are removed.
  const SECRET_QUERY = /([?&#](?:[^=&#]*(?:token|secret|password|session|auth|key|code|sig|signature|credential|csrf|xsrf|nonce|otp|jwt)[^=&#]*)=)[^&#]*/gi;
  const SOURCE_ATTRIBUTE = /^data-(source(-[\w-]+)?|inspector-[\w-]+|v-inspector|astro-source-[\w-]+|insp-path|locatorjs[\w-]*|component(-name)?|sentry-(component|source-file|element)|__source)$/;
  const TEST_ATTRIBUTES = ["data-testid", "data-test-id", "data-test", "data-cy", "data-qa"];

  // MARK: The composed tree: shadow roots and same-origin frames

  function frameDocument(element) {
    if (element.localName !== "iframe" && element.localName !== "frame") return null;
    try { return element.contentDocument || null; } catch { return null; }
  }

  // The element's parent across shadow roots and same-origin frames. The
  // top document's body has none: the page itself is the outermost scope.
  function parentOf(element) {
    if (!element || element === document.body || element === document.documentElement) return null;
    const parent = element.parentNode;
    if (parent instanceof ShadowRoot) return parent.host;
    if (parent && parent.nodeType === Node.ELEMENT_NODE) {
      if (parent === parent.ownerDocument.documentElement) return frameOf(parent.ownerDocument);
      return parent;
    }
    if (parent && parent.nodeType === Node.DOCUMENT_NODE) return frameOf(parent);
    return null;
  }

  // The frame element showing `doc`, or the top body for the top document.
  function frameOf(doc) {
    if (doc === document) return null;
    try { return doc.defaultView?.frameElement || null; } catch { return null; }
  }

  function rendered(element) {
    if (SKIPPED.has(element.localName)) return false;
    return element.getClientRects().length > 0;
  }

  function childrenOf(element) {
    const inner = frameDocument(element);
    if (inner) return inner.body ? [inner.body] : [];
    const root = element.shadowRoot || element;
    return [...root.children].filter(rendered);
  }

  // Where a document's viewport sits in the top viewport, in CSS pixels.
  function offsetOf(doc) {
    let x = 0, y = 0;
    for (let frame = frameOf(doc); frame; frame = frameOf(frame.ownerDocument)) {
      const box = frame.getBoundingClientRect();
      const style = frame.ownerDocument.defaultView.getComputedStyle(frame);
      x += box.left + frame.clientLeft + parseFloat(style.paddingLeft || "0");
      y += box.top + frame.clientTop + parseFloat(style.paddingTop || "0");
    }
    return { x, y };
  }

  function rectOf(element) {
    const box = element.getBoundingClientRect();
    const offset = offsetOf(element.ownerDocument);
    return { x: round(box.left + offset.x), y: round(box.top + offset.y), width: round(box.width), height: round(box.height) };
  }

  const round = (value) => Math.round(value * 10) / 10;

  // The innermost element at a point in the top viewport, through open
  // shadow roots and same-origin frames.
  function deepestAt(x, y) {
    let doc = document, origin = { x: 0, y: 0 };
    let element = doc.elementFromPoint(x, y);
    for (let guard = 0; element && guard < 64; guard++) {
      if (element.shadowRoot) {
        const inner = element.shadowRoot.elementFromPoint(x - origin.x, y - origin.y);
        if (inner && inner !== element) { element = inner; continue; }
      }
      const inner = frameDocument(element);
      if (inner) {
        const next = offsetOf(inner);
        const found = inner.elementFromPoint(x - next.x, y - next.y);
        if (found && found !== inner.documentElement) { doc = inner; origin = next; element = found; continue; }
      }
      break;
    }
    if (!element || element === document.documentElement || element === document.body) return null;
    if (element === element.ownerDocument.documentElement) return element.ownerDocument.body;
    return element;
  }

  function ancestorsOf(element) {
    const chain = [];
    for (let current = element; current; current = parentOf(current)) chain.push(current);
    return chain;
  }

  const near = (a, b) => Math.abs(a - b) <= 2;
  function sameBox(a, b) {
    const one = a.getBoundingClientRect(), two = b.getBoundingClientRect();
    return near(one.left, two.left) && near(one.top, two.top) && near(one.width, two.width) && near(one.height, two.height);
  }

  // The group holding an element: its nearest ancestor that isn't a
  // wrapper the same size as it, or null at the page.
  function groupOf(element) {
    let parent = parentOf(element);
    while (parent && sameBox(parent, element)) parent = parentOf(parent);
    return parent;
  }

  // Elements that are a thing of their own, though they flow in a line.
  const ATOMIC = new Set(["img", "svg", "video", "audio", "canvas", "picture", "iframe", "frame", "embed", "object",
    "input", "select", "textarea", "button", "meter", "progress"]);
  // Inline elements whose inline contents are part of them.
  const HOLDS_LABEL = new Set(["a", "button", "label", "summary"]);

  function isInline(element) {
    if (ATOMIC.has(element.localName)) return false;
    const display = element.ownerDocument.defaultView.getComputedStyle(element).display;
    return display === "inline" || display === "contents";
  }

  function hasOwnText(element) {
    for (const node of element.childNodes) {
      if (node.nodeType === Node.TEXT_NODE && collapse(node.data)) return true;
    }
    return false;
  }

  // An inline fragment of a line of text stands for the element holding
  // the text, so `$12<span>/mo</span>` outlines as the price, and a link's
  // bold word as the link.
  function textHolder(element) {
    let current = element;
    for (let guard = 0; guard < 64 && isInline(current); guard++) {
      const parent = current.parentElement;
      if (!parent || parent === document.body || parent === parent.ownerDocument.documentElement) break;
      if (!(isInline(parent) || HOLDS_LABEL.has(parent.localName) || hasOwnText(parent))) break;
      current = parent;
    }
    return current;
  }

  // What the pointer is over: the element directly under it, or the
  // innermost element when `deep`.
  function pickAt(x, y, deep) {
    const target = deepestAt(x, y);
    if (!target || deep) return target;
    return textHolder(target);
  }

  // MARK: Labels

  function stableClasses(element) {
    return [...element.classList].filter((name) => name && !GENERATED.test(name));
  }

  function label(element) {
    let text = element.localName;
    if (element.id) text += "#" + element.id;
    const classes = stableClasses(element).slice(0, 2);
    if (classes.length) text += "." + classes.join(".");
    return clip(text, LABEL_LIMIT);
  }

  // A short hint naming the element in source: its test ID, or the file
  // and line a dev tool's attribute gives.
  function hint(element) {
    for (const name of TEST_ATTRIBUTES) {
      const value = element.getAttribute(name);
      if (value) return clip(value, LABEL_LIMIT);
    }
    const source = element.getAttribute("data-inspector-relative-path") || element.getAttribute("data-astro-source-file")
      || element.getAttribute("data-source-file") || element.getAttribute("data-sentry-source-file")
      || (element.getAttribute("data-v-inspector") || element.getAttribute("data-insp-path") || element.getAttribute("data-source") || "").split(":")[0];
    const component = element.getAttribute("data-component") || element.getAttribute("data-component-name") || element.getAttribute("data-sentry-component");
    if (component) return clip(component, LABEL_LIMIT);
    if (source) {
      const line = element.getAttribute("data-inspector-line") || element.getAttribute("data-source-line")
        || (element.getAttribute("data-v-inspector") || element.getAttribute("data-insp-path") || element.getAttribute("data-source") || "").split(":")[1];
      return clip(source.split("/").pop() + (line ? ":" + line : ""), LABEL_LIMIT);
    }
    return null;
  }

  function target(element) {
    if (!element || !element.isConnected) return null;
    return { label: label(element), hint: hint(element), rect: rectOf(element) };
  }

  function crumbs() {
    if (!selected) return [];
    const chain = ancestorsOf(selected).slice(0, CRUMB_LEVELS).reverse();
    return chain.map(label);
  }

  function state() {
    if (selected && !selected.isConnected) { selected = null; trail = []; }
    if (hovered && !hovered.isConnected) hovered = null;
    const group = selected ? groupOf(selected) : null;
    return {
      hovered: hovered && hovered !== selected ? target(hovered) : null,
      selected: target(selected),
      scope: group && group !== document.body ? target(group) : null,
      crumbs: crumbs(),
      canParent: !!selected && groupOf(selected) != null,
      canChild: !!selected && (trail.length > 0 || childrenOf(selected).length > 0),
      viewport: { width: window.innerWidth, height: window.innerHeight },
    };
  }

  // MARK: Operations

  function hover(args) {
    hovered = pickAt(args.x, args.y, args.deep === true);
    return state();
  }

  function select(args) {
    const picked = pickAt(args.x, args.y, args.deep === true);
    if (picked !== selected) { selected = picked; trail = []; }
    hovered = null;
    return state();
  }

  // Parent steps out past wrappers the same size; child steps back in the
  // way it came, or into the first child.
  function step(args) {
    if (!selected) return state();
    if (args.to === "parent") {
      const group = groupOf(selected);
      if (group) { trail.push(selected); selected = group; }
    } else if (args.to === "child") {
      const next = trail.pop() || childrenOf(selected)[0];
      if (next) selected = next;
    } else if (args.to === "crumb") {
      const chain = ancestorsOf(selected).slice(0, CRUMB_LEVELS).reverse();
      const wanted = chain[args.index];
      if (wanted && wanted !== selected) {
        for (let current = selected; current && current !== wanted; current = parentOf(current)) trail.push(current);
        selected = wanted;
      }
    }
    hovered = null;
    return state();
  }

  function clear() {
    hovered = null; selected = null; trail = [];
    return state();
  }

  // MARK: Capture

  // A selector unique within the element's own document or shadow root.
  function selectorFor(element) {
    const root = element.getRootNode();
    const unique = (selector) => {
      try {
        const found = root.querySelectorAll(selector);
        return found.length === 1 && found[0] === element;
      } catch { return false; }
    };
    const attribute = (name, value) => "[" + name + "=" + JSON.stringify(value) + "]";
    if (element.id && !GENERATED.test(element.id)) {
      const selector = "#" + CSS.escape(element.id);
      if (unique(selector)) return selector;
    }
    for (const name of TEST_ATTRIBUTES) {
      const value = element.getAttribute(name);
      if (value && unique(attribute(name, value))) return attribute(name, value);
    }
    const parts = [];
    for (let current = element; current && current.nodeType === Node.ELEMENT_NODE; current = current.parentElement) {
      if (current !== element && current.id && !GENERATED.test(current.id)) {
        const anchored = ["#" + CSS.escape(current.id), ...parts].join(" > ");
        if (unique(anchored)) return clip(anchored, SELECTOR_LIMIT);
      }
      // The plainest step first; a position only when it's needed.
      const plain = [stepFor(current, false), ...parts].join(" > ");
      if (unique(plain)) return clip(plain, SELECTOR_LIMIT);
      parts.unshift(stepFor(current, true));
      const selector = parts.join(" > ");
      if (unique(selector)) return clip(selector, SELECTOR_LIMIT);
      if (current.localName === "body" || current.localName === "html") break;
    }
    return clip(parts.join(" > "), SELECTOR_LIMIT);
  }

  function stepFor(element, positioned) {
    let step = element.localName;
    const classes = stableClasses(element).slice(0, 2);
    if (classes.length) step += "." + classes.map((name) => CSS.escape(name)).join(".");
    const parent = element.parentElement;
    if (positioned && parent) {
      const same = [...parent.children].filter((child) => child.localName === element.localName);
      if (same.length > 1) step += ":nth-of-type(" + (same.indexOf(element) + 1) + ")";
    }
    return step;
  }

  // The human path from the page's body to the element, nearest levels
  // kept, with shadow and frame boundaries marked.
  function pathFor(element) {
    const SHADOW = null;
    const parts = [];
    let current = element, more = false;
    while (current) {
      if (parts.length >= PATH_LEVELS) { more = true; break; }
      parts.unshift(label(current));
      const parent = current.parentNode;
      if (parent instanceof ShadowRoot) { parts.unshift(SHADOW); current = parent.host; continue; }
      if (current.localName === "body" || !current.parentElement) break;
      current = current.parentElement;
    }
    let text = more ? "…" : "";
    for (const part of parts) {
      if (part === SHADOW) text += " >>shadow>>";
      else text += (text === "" ? "" : text.endsWith(">>") ? " " : " > ") + part;
    }
    return text;
  }

  function isTextEntry(element) {
    return element.localName === "input" || element.localName === "textarea" || element.localName === "select"
      || (element.isContentEditable === true);
  }

  // Visible text, never a field's value or an editable region's contents.
  function visibleText(element) {
    if (isTextEntry(element)) return "";
    const doc = element.ownerDocument;
    const walker = doc.createTreeWalker(element, NodeFilter.SHOW_TEXT);
    let text = "", visited = 0;
    for (let node = walker.nextNode(); node && visited < 2000 && text.length < TEXT_LIMIT + 1; node = walker.nextNode()) {
      visited++;
      const parent = node.parentElement;
      if (!parent || parent.closest("script, style, noscript, template, textarea, select, [contenteditable]:not([contenteditable=false])")) continue;
      if (typeof parent.checkVisibility === "function" && !parent.checkVisibility({ visibilityProperty: true })) continue;
      text += " " + node.data;
      text = collapse(text);
    }
    return clip(collapse(text), TEXT_LIMIT);
  }

  function attributesOf(element) {
    const kept = [];
    for (const attribute of element.attributes) {
      const name = attribute.name;
      if (SECRET_NAME.test(name) || name === "id" || name === "class" || name === "style" || name.startsWith("on")) continue;
      if (SOURCE_ATTRIBUTE.test(name)) continue;
      const wanted = name.startsWith("data-") || name.startsWith("aria-")
        || ["role", "href", "src", "alt", "title", "name", "type", "for", "placeholder", "target", "rel", "action", "method", "tabindex", "disabled", "hidden"].includes(name);
      if (!wanted) continue;
      const value = URL_ATTRIBUTES.has(name) ? redact(attribute.value) : attribute.value;
      kept.push([name, clip(collapse(value), ATTRIBUTE_VALUE_LIMIT)]);
      if (kept.length >= ATTRIBUTE_LIMIT) break;
    }
    return kept;
  }

  // Dev tooling's source-location attributes, from the element or its
  // nearest ancestor that has them.
  function sourceOf(element) {
    let distance = 0;
    for (let current = element; current && distance < 16; current = parentOf(current), distance++) {
      const found = [...current.attributes].filter((attribute) => SOURCE_ATTRIBUTE.test(attribute.name))
        .slice(0, 6).map((attribute) => [attribute.name, clip(collapse(attribute.value), ATTRIBUTE_VALUE_LIMIT)]);
      if (found.length) return { attributes: found, on: distance === 0 ? null : label(current), distance };
    }
    return null;
  }

  function stylesOf(element) {
    const style = element.ownerDocument.defaultView.getComputedStyle(element);
    const styles = [["display", style.display], ["position", style.position]];
    const zero = (value) => !value || /^(0px ?)+$/.test(value.trim()) || value === "0px";
    styles.push(["color", style.color]);
    if (style.backgroundColor && style.backgroundColor !== "rgba(0, 0, 0, 0)" && style.backgroundColor !== "transparent") {
      styles.push(["background", style.backgroundColor]);
    }
    const family = (style.fontFamily || "").split(",")[0].replace(/["']/g, "").trim();
    styles.push(["font", style.fontWeight + " " + style.fontSize + "/" + style.lineHeight + " " + family]);
    if (!zero(style.padding)) styles.push(["padding", style.padding]);
    if (!zero(style.margin)) styles.push(["margin", style.margin]);
    if (!zero(style.borderRadius)) styles.push(["border-radius", style.borderRadius]);
    if (style.display.includes("flex") || style.display.includes("grid")) {
      if (style.gap && style.gap !== "normal") styles.push(["gap", style.gap]);
    }
    if (style.zIndex && style.zIndex !== "auto") styles.push(["z-index", style.zIndex]);
    if (style.opacity && style.opacity !== "1") styles.push(["opacity", style.opacity]);
    return styles.map(([name, value]) => [name, clip(collapse(value), 80)]);
  }

  const VOID = new Set(["area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "source", "track", "wbr"]);

  // A sanitized rendering of the element's HTML: no field values, secret
  // attributes, scripts, or editable contents, bounded in depth and size.
  function htmlOf(element) {
    let out = "";
    let truncated = false;
    const escapeText = (text) => text.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
    const escapeValue = (text) => escapeText(text).replace(/"/g, "&quot;");
    const write = (text) => {
      if (out.length + text.length > HTML_LIMIT) { truncated = true; return false; }
      out += text;
      return true;
    };
    const walk = (node, depth) => {
      if (truncated) return;
      if (node.nodeType === Node.TEXT_NODE) {
        const text = collapse(node.data);
        if (text) write(escapeText(clip(text, 80)));
        return;
      }
      if (node.nodeType !== Node.ELEMENT_NODE) return;
      const tag = node.localName;
      let open = "<" + tag;
      for (const attribute of node.attributes) {
        const name = attribute.name;
        // Source attributes are named once, on the Source line.
        if (SECRET_NAME.test(name) || name.startsWith("on") || SOURCE_ATTRIBUTE.test(name)) continue;
        const raw = URL_ATTRIBUTES.has(name) ? redact(attribute.value) : attribute.value;
        const value = clip(collapse(raw), name === "style" ? 80 : 120);
        open += value === "" ? " " + name : " " + name + "=\"" + escapeValue(value) + "\"";
      }
      open += ">";
      if (!write(open)) return;
      if (VOID.has(tag)) return;
      const opaque = ["script", "style", "textarea", "select", "noscript", "template"].includes(tag)
        || (node.isContentEditable && node.parentElement?.isContentEditable !== true);
      if (opaque || tag === "svg") {
        if (node.childNodes.length) write("…");
      } else if (depth >= 3) {
        if (node.children.length || collapse(node.textContent)) write("…");
      } else {
        let count = 0;
        for (const child of node.childNodes) {
          if (child.nodeType === Node.ELEMENT_NODE && ++count > 8) { write("…"); break; }
          walk(child, depth + 1);
          if (truncated) return;
        }
      }
      write("</" + tag + ">");
    };
    walk(element, 0);
    if (truncated) out += "…";
    return { html: out, truncated };
  }

  // MARK: Scrolling

  // How an element scrolls on each axis it can: its overflow, the size it
  // shows and holds, where it's scrolled to, and the space its scroll bar
  // takes, which is 0 for overlay scroll bars. Null when it doesn't scroll:
  // `scroll` shows bars even when its contents fit; `auto` only when they
  // overflow.
  function scrollingOf(element) {
    const style = element.ownerDocument.defaultView.getComputedStyle(element);
    const axis = (overflow, visible, content, at, bar) => {
      const scrolls = overflow === "scroll" || ((overflow === "auto" || overflow === "overlay") && content > visible + 1);
      return scrolls ? { overflow, visible: round(visible), content: round(content), at: round(at), bar: Math.max(0, round(bar)) } : null;
    };
    const border = (side) => parseFloat(style["border" + side + "Width"] || "0");
    const vertical = axis(style.overflowY, element.clientHeight, element.scrollHeight, element.scrollTop,
      element.offsetWidth - element.clientWidth - border("Left") - border("Right"));
    const horizontal = axis(style.overflowX, element.clientWidth, element.scrollWidth, element.scrollLeft,
      element.offsetHeight - element.clientHeight - border("Top") - border("Bottom"));
    return vertical || horizontal ? { vertical, horizontal } : null;
  }

  // The nearest element around `element` in its document that scrolls,
  // and how much of `element` it shows. The page's own scrolling is the
  // viewport's.
  function scrollerOf(element) {
    const doc = element.ownerDocument;
    for (let current = parentOf(element); current && current.ownerDocument === doc; current = parentOf(current)) {
      if (current === doc.body || current === doc.documentElement) return null;
      const scrolling = scrollingOf(current);
      if (!scrolling) continue;
      const box = current.getBoundingClientRect();
      const left = box.left + current.clientLeft, top = box.top + current.clientTop;
      const right = left + current.clientWidth, bottom = top + current.clientHeight;
      const own = element.getBoundingClientRect();
      const inside = own.left >= left - 1 && own.right <= right + 1 && own.top >= top - 1 && own.bottom <= bottom + 1;
      const apart = own.right <= left || own.left >= right || own.bottom <= top || own.top >= bottom;
      return { label: label(current), ...scrolling, shows: inside ? "all" : apart ? "none" : "part" };
    }
    return null;
  }

  function frameFor(element) {
    const doc = element.ownerDocument;
    if (doc === document) {
      if (element.localName === "iframe" || element.localName === "frame") {
        if (!frameDocument(element)) return { kind: "cross-origin", selector: null, src: clip(redact(element.getAttribute("src") || ""), 200) };
      }
      return { kind: "top", selector: null, src: null };
    }
    const frame = frameOf(doc);
    return { kind: "same-origin", selector: frame ? selectorFor(frame) : null, src: clip(redact(doc.location?.href || ""), 200) };
  }

  function shadowHosts(element) {
    const hosts = [];
    for (let root = element.getRootNode(); root instanceof ShadowRoot; root = root.host.getRootNode()) {
      hosts.unshift(selectorFor(root.host));
    }
    return hosts;
  }

  function capture() {
    if (!selected || !selected.isConnected) return { error: "Nothing is selected." };
    const element = selected;
    const style = element.ownerDocument.defaultView.getComputedStyle(element);
    const markup = htmlOf(element);
    return {
      page: {
        url: redact(location.href), title: clip(collapse(document.title), 200),
        viewport: { width: window.innerWidth, height: window.innerHeight },
        scroll: { x: Math.round(window.scrollX), y: Math.round(window.scrollY) },
        content: { width: round(document.documentElement.scrollWidth), height: round(document.documentElement.scrollHeight) },
      },
      scroller: scrollerOf(element),
      frame: frameFor(element),
      shadowHosts: shadowHosts(element),
      element: {
        tag: element.localName,
        id: element.id ? clip(element.id, LABEL_LIMIT) : null,
        classes: [...element.classList].slice(0, 12).map((name) => clip(name, 60)),
        role: element.getAttribute("role") || null,
        label: label(element),
        text: visibleText(element),
        selector: selectorFor(element),
        path: pathFor(element),
        rect: rectOf(element),
        position: ["fixed", "sticky"].includes(style.position) ? style.position : null,
        scrolling: scrollingOf(element),
        attributes: attributesOf(element),
        styles: stylesOf(element),
        html: markup.html,
        htmlTruncated: markup.truncated,
      },
      source: sourceOf(element),
    };
  }

  return {
    run(op, args) {
      switch (op) {
        case "hover": return JSON.stringify(hover(args));
        case "select": return JSON.stringify(select(args));
        case "step": return JSON.stringify(step(args));
        case "state": return JSON.stringify(state());
        case "clear": return JSON.stringify(clear());
        case "capture": return JSON.stringify(capture());
      }
      return JSON.stringify({ error: "Unknown operation." });
    },
  };
})();
