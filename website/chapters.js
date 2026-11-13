// Mark a chapter seen once it's mostly on screen, so its scene can settle in.
(() => {
  const chapters = [...document.querySelectorAll(".chapter[id]")];
  const io = new IntersectionObserver(
    (entries) => {
      for (const e of entries) if (e.isIntersecting) e.target.classList.add("is-seen");
    },
    { rootMargin: "-40% 0px -40% 0px" },
  );
  chapters.forEach((c) => io.observe(c));
})();
