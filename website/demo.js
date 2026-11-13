// A scripted Kit. Real DOM, real interaction, no model.
(() => {
  const $ = (id) => document.getElementById(id);
  const transcript = $("transcript");
  const empty = $("empty");
  const pending = $("pending");
  const pendingText = $("pending-text");
  const chips = $("chips");
  const suggest = $("suggest");
  const form = $("form");
  const input = $("input");
  const pane = $("pane");
  const paneTitle = $("pane-title");
  const paneMeta = $("pane-meta");
  const ctx = $("ctx");
  const footLeft = $("foot-left");
  const hint = $("hint");
  const kit = $("kit");
  const reduce = matchMedia("(prefers-reduced-motion: reduce)").matches;

  const sleep = (ms) => new Promise((r) => setTimeout(r, reduce ? 0 : ms));
  const esc = (s) => s.replace(/[&<>]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;" }[c]));
  // One pass, so a class attribute never gets re-tokenized as a string.
  const hl = (code) =>
    code.replace(/"[^"]*"|\b(func|if|return|var|for|range|error|string|bool)\b|\b(nil|err|ok)\b|(\w+)(?=\()|[&<>]/g, (m, kw, id, fn) => {
      if (kw) return `<i class="kw">${kw}</i>`;
      if (id) return `<i class="id">${id}</i>`;
      if (fn) return `<i class="fn">${fn}</i>`;
      if (m[0] === '"') return `<i class="str">${esc(m)}</i>`;
      return esc(m);
    });

  let usage = 4;
  const bump = (n) => {
    usage = Math.min(usage + n, 92);
    ctx.textContent = usage + "%";
    ctx.className = usage > 90 ? "k-err" : usage > 80 ? "k-warn" : "k-ok";
  };

  const scrollDown = () => {
    transcript.scrollTo({ top: transcript.scrollHeight, behavior: reduce ? "auto" : "smooth" });
  };

  const add = (html, cls) => {
    empty.hidden = true;
    const el = document.createElement("div");
    el.className = cls;
    el.innerHTML = html;
    transcript.appendChild(el);
    requestAnimationFrame(() => el.classList.add("is-in"));
    scrollDown();
    return el;
  };

  // --- Scrollbars show while an area is being scrolled, like macOS overlays.
  for (const el of document.querySelectorAll(".transcript, .pane, .phone-body")) {
    let t = 0;
    el.addEventListener(
      "scroll",
      () => {
        el.classList.add("is-scrolling");
        clearTimeout(t);
        t = setTimeout(() => el.classList.remove("is-scrolling"), 700);
      },
      { passive: true },
    );
  }

  // --- Pending slot: streams the latest thinking line, like Kit.
  const think = async (lines) => {
    pending.hidden = false;
    for (const line of lines) {
      pendingText.textContent = "";
      for (const ch of line) {
        pendingText.textContent += ch;
        await sleep(14);
      }
      await sleep(420);
    }
    pending.hidden = true;
  };

  // --- Tool group: one row per call, spinner then check, facts trailing.
  const tools = async (items) => {
    const group = add(
      `<div class="t-head"><span class="t-chev">▾</span><span>${items.length} tool call${items.length > 1 ? "s" : ""}</span></div><div class="t-rows"></div>`,
      "k-tools",
    );
    const rows = group.querySelector(".t-rows");
    for (const it of items) {
      const row = document.createElement("div");
      row.className = "t-row";
      row.innerHTML = `<span class="t-state"><span class="spin"></span></span><span class="t-chip">${it.name}</span><span class="t-title">${esc(it.title)}</span><span class="t-fact k-muted"></span>`;
      rows.appendChild(row);
      scrollDown();
      footLeft.textContent = it.name + " · " + it.title;
      await it.run?.();
      await sleep(it.ms ?? 500);
      row.querySelector(".t-state").innerHTML = '<span class="k-ok">✓</span>';
      row.querySelector(".t-fact").textContent = it.fact ?? "";
      bump(2);
    }
    footLeft.textContent = "";
    group.querySelector(".t-chev").textContent = "▸";
    group.classList.add("is-collapsed");
    group.querySelector(".t-head").addEventListener("click", () => group.classList.toggle("is-collapsed"));
    const facts = items.map((i) => i.name).join(" · ");
    group.querySelector(".t-head").insertAdjacentHTML("beforeend", `<span class="k-muted t-summary">${facts}</span>`);
  };

  // --- Diff pane
  let currentLines = null;
  const renderDiff = (file, hunk, lines, flash) => {
    currentLines = lines;
    paneTitle.textContent = "Diff · working tree";
    const adds = lines.filter((l) => l.k === "add").length;
    const dels = lines.filter((l) => l.k === "del").length;
    paneMeta.innerHTML = `<i class="k-ok">+${adds}</i> <i class="k-err">−${dels}</i>`;
    let html = `<div class="d-file"><span>${file}</span><span class="k-muted">unified</span></div><div class="d-hunk">${esc(hunk)}</div>`;
    lines.forEach((l, i) => {
      html += `<div class="d-line ${l.k}${flash && flash.includes(i) ? " is-new" : ""}" data-i="${i}"><button class="d-plus" type="button" title="Comment on this line">+</button><span class="d-ln">${l.n ?? ""}</span><span class="d-code">${l.k === "add" ? "+" : l.k === "del" ? "−" : " "} ${hl(l.c)}</span></div>`;
    });
    pane.innerHTML = html;
    pane.scrollTop = 0;
  };

  const DIFF_V1 = [
    { k: "ctx", n: 38, c: "target, err := s.resolve(path)" },
    { k: "ctx", n: 39, c: "if err != nil {" },
    { k: "ctx", n: 40, c: "\treturn err" },
    { k: "ctx", n: 41, c: "}" },
    { k: "del", n: 42, c: "s.publish(target)" },
    { k: "del", n: 43, c: "return s.store.SaveCwd(ctx, target)" },
    { k: "add", n: 42, c: "receipt := newReceipt(requestID, target)" },
    { k: "add", n: 43, c: "if err := s.store.SaveCwd(ctx, receipt); err != nil {" },
    { k: "add", n: 44, c: "\treturn err" },
    { k: "add", n: 45, c: "}" },
    { k: "add", n: 46, c: "s.publish(target)" },
    { k: "ctx", n: 47, c: "return nil" },
  ];
  const DIFF_V2 = [
    { k: "ctx", n: 38, c: "target, err := s.resolve(path)" },
    { k: "ctx", n: 39, c: "if err != nil {" },
    { k: "ctx", n: 40, c: "\treturn err" },
    { k: "ctx", n: 41, c: "}" },
    { k: "del", n: 42, c: "s.publish(target)" },
    { k: "del", n: 43, c: "return s.store.SaveCwd(ctx, target)" },
    { k: "add", n: 42, c: "if prior, ok := s.receipts[requestID]; ok {" },
    { k: "add", n: 43, c: "\treturn prior.Err" },
    { k: "add", n: 44, c: "}" },
    { k: "add", n: 45, c: "receipt := newReceipt(requestID, target)" },
    { k: "add", n: 46, c: "if err := s.store.SaveCwd(ctx, receipt); err != nil {" },
    { k: "add", n: 47, c: "\treturn err" },
    { k: "add", n: 48, c: "}" },
    { k: "add", n: 49, c: "s.publish(target)" },
    { k: "ctx", n: 50, c: "return nil" },
  ];

  // --- Notes: click + in the gutter, write, save; chips above the composer.
  const notes = [];
  pane.addEventListener("click", (e) => {
    const plus = e.target.closest(".d-plus");
    if (!plus) return;
    const line = plus.closest(".d-line");
    if (line.nextElementSibling?.classList.contains("d-note-editor")) return;
    const i = +line.dataset.i;
    const n = currentLines[i].n;
    const ed = document.createElement("div");
    ed.className = "d-note-editor";
    ed.innerHTML = `<textarea rows="2" placeholder="Note for the agent…">Check for an existing receipt first, or a retried request moves twice.</textarea><div class="d-note-actions"><button type="button" class="save">Save</button><button type="button" class="cancel k-muted">Cancel</button></div>`;
    line.after(ed);
    const ta = ed.querySelector("textarea");
    ta.focus();
    ta.setSelectionRange(0, 0);
    ed.querySelector(".cancel").onclick = () => ed.remove();
    ed.querySelector(".save").onclick = () => {
      const text = ta.value.trim();
      if (!text) return ed.remove();
      const note = { file: "cwd.go", line: n, text };
      notes.push(note);
      const box = document.createElement("div");
      box.className = "d-note";
      box.innerHTML = `<span class="d-note-anchor">🗨 L${n}</span><span>${esc(text)}</span>`;
      ed.replaceWith(box);
      const chip = document.createElement("span");
      chip.className = "chip";
      chip.innerHTML = `<span>🗨 cwd.go L${n}</span><button type="button" aria-label="Remove note">×</button>`;
      chip.querySelector("button").onclick = () => {
        notes.splice(notes.indexOf(note), 1);
        chip.remove();
        box.remove();
      };
      chips.appendChild(chip);
      input.placeholder = "Send the note, or add more…";
    };
  });

  // --- Scripts
  const user = (text, noteList) => {
    let html = esc(text);
    if (noteList?.length) {
      html += `<div class="u-notes">${noteList.map((n) => `<span>🗨 ${n.file} L${n.line} · ${esc(n.text)}</span>`).join("")}</div>`;
    }
    add(html, "k-user");
    bump(3);
  };
  const assistant = (html) => {
    add(html, "k-assistant");
    bump(4);
  };

  const scripts = {
    async fix() {
      await think(["Reading ChangeCwd and the receipt store…", "A retried request re-runs the whole body. SaveCwd should be keyed by request ID."]);
      await tools([
        { name: "read", title: "internal/session/cwd.go", fact: "88 lines", ms: 600 },
        { name: "read", title: "internal/session/receipt.go", fact: "41 lines", ms: 450 },
        {
          name: "edit",
          title: "internal/session/cwd.go",
          fact: "+5 −2",
          ms: 700,
          run: async () => renderDiff("internal/session/cwd.go", "@@ -38,9 +38,12 @@ func (s *Session) ChangeCwd(", DIFF_V1, [6, 7, 8, 9, 10]),
        },
        { name: "bash", title: "go test ./internal/session", fact: "ok · 1.2s", ms: 900 },
      ]);
      assistant(`<b>Receipts</b><p>ChangeCwd now stores a receipt keyed by the request ID before it publishes, so a client that retries gets the same outcome instead of moving twice. Tests pass.</p>`);
      hint.innerHTML = "Press <b>+</b> on a line to leave a note.";
      pane.classList.add("is-reviewable");
    },
    async review(noteList) {
      pane.classList.remove("is-reviewable");
      await think(["The note is right: the guard has to come before SaveCwd, not after.", "Adding an early return and a test that retries mid-move."]);
      await tools([
        {
          name: "edit",
          title: "internal/session/cwd.go",
          fact: "+4",
          ms: 700,
          run: async () => renderDiff("internal/session/cwd.go", "@@ -38,9 +38,16 @@ func (s *Session) ChangeCwd(", DIFF_V2, [6, 7, 8]),
        },
        { name: "edit", title: "internal/session/cwd_test.go", fact: "+22", ms: 600 },
        { name: "bash", title: "go test -run Retry ./internal/session", fact: "ok · 0.8s", ms: 900 },
      ]);
      assistant(`<b>Guarded</b><p>ChangeCwd returns the prior receipt when a request ID repeats, before anything is saved or published. Added <code>TestChangeCwdRetryIsIdempotent</code>, which interrupts the first attempt after SaveCwd and retries; the cwd moves once.</p>`);
      hint.innerHTML = "Try <b>Step away</b>.";
    },
    async explain() {
      await think(["Sessions are rows in the server's SQLite, not state in a client…"]);
      await tools([
        { name: "read", title: "docs/adrs/0001-native-go-architecture.md", fact: "212 lines", ms: 600 },
        { name: "read", title: "internal/session/store.go", fact: "340 lines", ms: 500 },
      ]);
      assistant(`<b>Sessions live in the server</b><p>Every message, tool call, and note is written to the Kit server's SQLite as it happens. A client only renders. Close the terminal, or the laptop, and the turn keeps running; reopen from any client and it resumes from the stored transcript. Restart the server and it picks up where the log ends.</p><pre class="k-fig">terminal ─┐
Mac app  ─┼─▶ Kit server ─▶ SQLite ─▶ sessions, subagents, notes
iPhone   ─┘   (this Mac, or me@devbox over SSH)</pre>`);
      hint.innerHTML = "Try <b>Step away</b>.";
    },
    async diagram() {
      await think(["A sequence diagram, drawn by Kit's own renderer."]);
      assistant(`<p>A review round-trips like this:</p><pre class="k-fig">┌──────┐                    ┌─────┐
│ You  │                    │ Kit │
└──┬───┘                    └──┬──┘
   │  note on cwd.go L42–44    │
   │──────────────────────────▶│
   │                           │ edit, test
   │  fix + TestRetryIdempotent│
   │◀──────────────────────────│
   │  ✓ looks good             │
   │──────────────────────────▶│</pre><p class="k-muted">In the Mac app this is drawn natively; in the terminal, with box-drawing characters.</p>`);
      hint.innerHTML = "Try <b>Fix the double-move</b> to see the diff pane and leave a note.";
    },
    async other(text) {
      await think(["This Kit is scripted, so I can't do that here…"]);
      assistant(`<p>This is a scripted demo. The real Kit would take that on: <code>brew install akonwi/tap/kit</code>, then <code>kit</code> in your repo.</p>`);
    },
  };

  let busy = false;
  const run = async (text, task) => {
    if (busy) return;
    busy = true;
    suggest.hidden = true;
    kit.classList.add("is-busy");
    const noteList = notes.splice(0);
    chips.innerHTML = "";
    user(text, noteList);
    input.value = "";
    input.placeholder = "Queue a follow-up…";
    if (noteList.length) await scripts.review(noteList);
    else if (task && scripts[task]) await scripts[task]();
    else await scripts.other(text);
    input.placeholder = "Ask Kit to do something…";
    kit.classList.remove("is-busy");
    busy = false;
    if (!task || task === "explain" || task === "diagram") suggest.hidden = false;
  };

  suggest.addEventListener("click", (e) => {
    const b = e.target.closest("button[data-task]");
    if (b) run(b.textContent, b.dataset.task);
  });
  form.addEventListener("submit", (e) => {
    e.preventDefault();
    const text = input.value.trim();
    if (!text && notes.length) return run("Address these", null);
    if (!text) return;
    const t = text.toLowerCase();
    const task = /retr|receipt|double|move|fix|bug/.test(t) ? "fix" : /session|restart|server|close/.test(t) ? "explain" : /diagram|draw|review/.test(t) ? "diagram" : null;
    run(text, task);
  });

  // --- Step away: the same transcript on a phone.
  const phone = $("phone");
  const phoneBody = $("phone-body");
  $("away").addEventListener("click", () => {
    phoneBody.innerHTML = "";
    const items = [...transcript.querySelectorAll(".k-user, .k-assistant, .k-tools")];
    if (!items.length) {
      phoneBody.innerHTML = '<div class="k-empty"><span class="k-word">k i t</span><span class="k-rule">━━━━━━━━━━━</span><span class="k-muted">Nothing yet. Start a task on the Mac first.</span></div>';
    }
    for (const it of items) phoneBody.appendChild(it.cloneNode(true));
    phone.hidden = false;
    kit.classList.add("is-away");
    requestAnimationFrame(() => phoneBody.scrollTo(0, phoneBody.scrollHeight));
  });
  $("back").addEventListener("click", () => {
    kit.classList.remove("is-away");
    setTimeout(() => (phone.hidden = true), 260);
  });

  // --- Autoplay the first task once the window is mostly on screen.
  if (kit.dataset.autoplay !== undefined) {
    let played = false;
    const auto = new IntersectionObserver(
      (entries) => {
        if (played || !entries.some((e) => e.isIntersecting)) return;
        played = true;
        auto.disconnect();
        setTimeout(() => {
          const b = suggest.querySelector("button[data-task=\"fix\"]");
          if (b && !transcript.querySelector(".k-user")) b.click();
        }, 500);
      },
      { threshold: 0.6 },
    );
    auto.observe(kit);
  }
})();
