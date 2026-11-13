// The file view's find bar, under its header: the query with its count, the
// Match Case and Whole Word toggles, previous and next, and close.
import { type Accessor, Show } from "solid-js";
import { Icon } from "../ui/icons";
import { type FileFind, fieldFindAction } from "./fileFind";

export function FindBar(props: {
  find: FileFind;
  version: Accessor<number>;
  ref: (field: HTMLInputElement) => void;
}) {
  const read = <T,>(value: () => T) => {
    props.version();
    return value();
  };
  const query = () => read(() => props.find.query);
  const count = () => read(() => props.find.count);
  const matchCase = () => read(() => props.find.matchCase);
  const wholeWord = () => read(() => props.find.wholeWord);
  const note = () => read(() => props.find.note);
  const stepless = () => read(() => props.find.matches.length === 0);

  return (
    <search class="file-find">
      <div class="file-find-row">
        <div class="file-find-field">
          <input
            ref={props.ref}
            aria-label="Find in file"
            type="text"
            class="mono"
            enterkeyhint="search"
            autocomplete="off"
            autocapitalize="off"
            spellcheck={false}
            value={query()}
            onInput={(event) => props.find.setQuery(event.currentTarget.value)}
            onKeyDown={(event) => {
              const action = fieldFindAction(event);
              if (!action || event.isComposing) return;
              event.preventDefault();
              if (action === "next") props.find.next();
              else if (action === "previous") props.find.previous();
              else if (action === "close") props.find.close();
            }}
          />
          <span class="file-find-count" aria-live="polite">
            {count()}
          </span>
        </div>
        <button
          type="button"
          class="file-find-toggle"
          aria-label="Match Case"
          title="Match Case"
          aria-pressed={matchCase()}
          onClick={() => props.find.setMatchCase(!matchCase())}
        >
          Aa
        </button>
        <button
          type="button"
          class="file-find-toggle whole-word"
          aria-label="Whole Word"
          title="Whole Word"
          aria-pressed={wholeWord()}
          onClick={() => props.find.setWholeWord(!wholeWord())}
        >
          ab
        </button>
        <button
          type="button"
          class="icon-button file-find-step"
          aria-label="Previous match"
          disabled={stepless()}
          onClick={() => props.find.previous()}
        >
          <Icon name="chevUp" size={18} />
        </button>
        <button
          type="button"
          class="icon-button file-find-step"
          aria-label="Next match"
          disabled={stepless()}
          onClick={() => props.find.next()}
        >
          <Icon name="chevDown" size={18} />
        </button>
        <button
          type="button"
          class="icon-button file-find-step"
          aria-label="Close find"
          onClick={() => props.find.close()}
        >
          <Icon name="close" size={18} />
        </button>
      </div>
      <Show when={note()}>
        <p class="file-find-note">{note()}</p>
      </Show>
    </search>
  );
}
